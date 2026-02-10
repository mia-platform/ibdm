// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package pipeline

import (
	"context"
	"time"

	"github.com/mia-platform/ibdm/internal/destination"
	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/mapper"
	"github.com/mia-platform/ibdm/internal/server"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	loggerName = "ibdm:pipeline"
)

// dataPipeline represents a function that pushes source data onto a channel.
type dataPipeline = func(ctx context.Context, channel chan<- source.Data) error

// DataMapper couples a mapper with the metadata needed to build destination payloads.
type DataMapper struct {
	APIVersion string
	ItemFamily string
	Extra      source.Extra
	Mapper     mapper.Mapper
}

// Pipeline orchestrates the flow from a source through mappers into a destination.
type Pipeline struct {
	source        any
	mappers       map[string]DataMapper
	mapperTypes   map[string]source.Extra
	destination   destination.Sender
	serverCreator func(ctx context.Context) (server.Server, error)
}

// New wires together the given source, mappers, and destination into a Pipeline.
func New(ctx context.Context, src any, mappers map[string]DataMapper, destination destination.Sender) (*Pipeline, error) {
	mapperTypes := make(map[string]source.Extra, len(mappers))
	for dataType, mapping := range mappers {
		mapperTypes[dataType] = mapping.Extra
	}

	return &Pipeline{
		source:        src,
		mappers:       mappers,
		mapperTypes:   mapperTypes,
		destination:   destination,
		serverCreator: server.NewServer,
	}, nil
}

// Start begins streaming data from a source.EventSource or source.WebhookSource.
func (p *Pipeline) Start(ctx context.Context) error {
	log := logger.FromContext(ctx).WithName(loggerName)

	server, err := p.serverCreator(ctx)
	if err != nil {
		return err
	}

	streamSource, isStream := p.source.(source.EventSource)
	webhookSource, isWebhook := p.source.(source.WebhookSource)

	var dataPipeline dataPipeline
	switch {
	case isStream:
		dataPipeline = func(ctx context.Context, channel chan<- source.Data) error {
			// server start in different goroutine
			log.Trace("starting server")
			errChannel := server.StartAsync()
			go func() {
				if err := <-errChannel; err != nil {
					log.Error("server closed", "error", err)
					return
				}
				log.Trace("server closed")
			}()
			return streamSource.StartEventStream(ctx, p.mapperTypes, channel)
		}
	case isWebhook:
		dataPipeline = func(ctx context.Context, channel chan<- source.Data) error {
			// server start here and keeps pipeline alive, server error = pipeline error
			webhook, err := webhookSource.GetWebhook(ctx, p.mapperTypes, channel)
			if err != nil {
				return err
			}
			log.Trace("registering webhook")
			server.AddRoute(webhook.Method, webhook.Path, webhook.Handler)
			log.Trace("registered webhook, starting server")
			log.Trace("starting server")
			return server.Start()
		}
	default:
		return &unsupportedSourceError{
			Message: "source does not support either streaming or webhook data",
		}
	}

	log.Trace("starting data pipeline")
	err = p.runDataPipeline(ctx, dataPipeline)
	log.Trace("event stream finished")

	return err
}

// Sync performs a one-off synchronization using a source.SyncableSource.
func (p *Pipeline) Sync(ctx context.Context) error {
	log := logger.FromContext(ctx).WithName(loggerName)

	syncSource, ok := p.source.(source.SyncableSource)
	if !ok {
		return &unsupportedSourceError{
			Message: "source does not support sync operation",
		}
	}

	log.Trace("starting data synchronization")
	err := p.runDataPipeline(ctx, func(ctx context.Context, channel chan<- source.Data) error {
		return syncSource.StartSyncProcess(ctx, p.mapperTypes, channel)
	})
	log.Trace("synchronization finished")
	return err
}

// runDataPipeline runs dataPipeline and waits for the mapper goroutine to drain the channel.
func (p *Pipeline) runDataPipeline(ctx context.Context, dataPipeline dataPipeline) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	channel := make(chan source.Data)

	// mappingDone closes when the mapping goroutine finishes consuming the channel.
	mappingDone := make(chan struct{})
	go func() {
		log.Trace("starting data mapping process")
		p.mappingData(ctx, channel)
		log.Trace("closing data mapping process")
		close(mappingDone)
	}()

	err := dataPipeline(ctx, channel)
	close(channel)

	<-mappingDone
	return err
}

// Stop attempts a graceful shutdown when the source implements source.ClosableSource.
func (p *Pipeline) Stop(ctx context.Context, timeout time.Duration) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	closableSource, ok := p.source.(source.ClosableSource)
	if !ok {
		log.Debug("source does not implement ClosableSource, skipping close")
		return nil
	}

	log.Debug("stop source")
	return closableSource.Close(ctx, timeout)
}

// mappingData consumes channel entries, runs the matching mapper, and forwards results.
func (p *Pipeline) mappingData(ctx context.Context, channel <-chan source.Data) {
	log := logger.FromContext(ctx).WithName(loggerName)
	for {
		select {
		case <-ctx.Done():
			log.Debug("pipeline cancelled from context", "error", ctx.Err())
			return
		case data, ok := <-channel:
			if !ok {
				return
			}
			dataMapper, found := p.mappers[data.Type]
			if !found {
				log.Debug("data type not mapped, skipping", "type", data.Type)
				continue
			}

			log.Trace("sending data", "type", data.Type, "operation", data.Operation.String())
			dataToSend := &destination.Data{
				APIVersion:    dataMapper.APIVersion,
				ItemFamily:    dataMapper.ItemFamily,
				OperationTime: data.Timestamp(),
			}
			parentResourceInfo := mapper.ParentItemInfo{
				APIVersion: dataMapper.APIVersion,
				ItemFamily: dataMapper.ItemFamily,
			}
			switch data.Operation {
			case source.DataOperationUpsert:
				output, extra, err := dataMapper.Mapper.ApplyTemplates(data.Values, parentResourceInfo)
				if err != nil {
					log.Error("error applying mapper templates", "type", data.Type, "error", err)
					continue
				}
				dataToSend.Name = output.Identifier
				if output.Metadata != nil {
					dataToSend.Metadata = output.Metadata
				}
				dataToSend.Data = output.Spec
				if err := p.destination.SendData(ctx, dataToSend); err != nil {
					log.Error("error sending data to destination", "type", data.Type, "error", err)
					continue
				}
				p.upsertExtraMappedData(ctx, data, extra)
			case source.DataOperationDelete:
				identifier, extra, err := dataMapper.Mapper.ApplyIdentifierTemplate(data.Values)
				dataToSend.Name = identifier
				if err != nil {
					log.Error("error applying mapper templates", "type", data.Type, "error", err)
					continue
				}
				if err := p.destination.DeleteData(ctx, dataToSend); err != nil {
					log.Error("error deleting data from destination", "type", data.Type, "error", err)
					continue
				}
				p.deleteExtraMappedData(ctx, data, extra)
			}

			log.Trace("data sent", "type", data.Type, "operation", data.Operation.String())
		}
	}
}

func (p *Pipeline) upsertExtraMappedData(ctx context.Context, data source.Data, extra []mapper.ExtraMappedData) {
	log := logger.FromContext(ctx).WithName(loggerName)
	for _, extraOutput := range extra {
		extraDataToSend := &destination.Data{
			APIVersion:    extraOutput.APIVersion,
			ItemFamily:    extraOutput.ItemFamily,
			OperationTime: data.Timestamp(),
			Name:          extraOutput.Identifier,
			Data:          extraOutput.Spec,
		}
		log.Trace("sending data", "type", extraOutput.ItemFamily, "operation", data.Operation.String())
		if err := p.destination.SendData(ctx, extraDataToSend); err != nil {
			log.Error("error sending extra data to destination", "type", extraOutput.ItemFamily, "error", err)
			continue
		}
	}
}

func (p *Pipeline) deleteExtraMappedData(ctx context.Context, data source.Data, extra []mapper.ExtraMappedData) {
	log := logger.FromContext(ctx).WithName(loggerName)

	if len(extra) > 0 {
		for _, extraIdentifier := range extra {
			extraDataToDelete := &destination.Data{
				APIVersion:    extraIdentifier.APIVersion,
				ItemFamily:    extraIdentifier.ItemFamily,
				OperationTime: data.Timestamp(),
				Name:          extraIdentifier.Identifier,
			}
			log.Trace("sending data", "type", extraDataToDelete.ItemFamily, "operation", data.Operation.String())
			if err := p.destination.DeleteData(ctx, extraDataToDelete); err != nil {
				log.Error("error deleting extra data from destination", "type", extraDataToDelete.ItemFamily, "error", err)
				continue
			}
		}
	}
}
