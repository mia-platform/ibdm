// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package pipeline

import (
	"context"
	"slices"
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
	// Name identifies the mapping within the loaded set.
	Name       string
	APIVersion string
	ItemFamily string
	Extra      source.Extra
	Mapper     mapper.Mapper
}

// Pipeline orchestrates the flow from a source through mappers into a destination.
type Pipeline struct {
	source        any
	mappers       map[string][]DataMapper
	mapperTypes   map[string]source.MappingExtras
	destination   destination.Sender
	serverCreator func(ctx context.Context) (server.Server, error)
}

// New wires together the given source, mappers, and destination into a Pipeline.
// mappers groups the mappings by data type. Every mapping registered for a type
// renders each emission of that type, in slice order.
func New(ctx context.Context, src any, mappers map[string][]DataMapper, destination destination.Sender) (*Pipeline, error) {
	return &Pipeline{
		source:        src,
		mappers:       mappers,
		mapperTypes:   mappingExtras(mappers),
		destination:   destination,
		serverCreator: server.NewServer,
	}, nil
}

// mappingExtras builds the configuration handed to sources: for every type, the
// extra of each mapping registered for it, keyed by mapping name. A type without
// any mapping is left out, so sources are never asked for data nobody maps.
func mappingExtras(mappers map[string][]DataMapper) map[string]source.MappingExtras {
	mapperTypes := make(map[string]source.MappingExtras, len(mappers))
	for dataType, dataMappers := range mappers {
		if len(dataMappers) == 0 {
			continue
		}

		extras := make(source.MappingExtras, len(dataMappers))
		for _, dataMapper := range dataMappers {
			extras[dataMapper.Name] = dataMapper.Extra
		}
		mapperTypes[dataType] = extras
	}

	return mapperTypes
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

// mappingData consumes channel entries, runs every mapper registered for their type that
// the entry targets, and forwards the results.
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
			dataMappers := p.mappers[data.Type]
			if len(dataMappers) == 0 {
				log.Debug("data type not mapped, skipping", "type", data.Type)
				continue
			}

			for _, dataMapper := range targetedMappers(log, data, dataMappers) {
				p.applyMapper(ctx, data, dataMapper)
			}
		}
	}
}

// targetedMappers selects the mappers data targets among dataMappers, the mappers registered
// for its type. A nil data.Mappings selects every mapper. Otherwise the mappers it names are
// selected, in registration order and each once; a name matching no mapper of the type is
// reported and ignored. A non-nil empty data.Mappings is a source bug: it is reported and
// selects nothing, because rendering every mapping for data meant for none could write items
// built from a payload their templates were not written for.
func targetedMappers(log logger.Logger, data source.Data, dataMappers []DataMapper) []DataMapper {
	if data.Mappings == nil {
		return dataMappers
	}

	if len(data.Mappings) == 0 {
		log.Error("source emitted data targeting no mapping, dropping it", "type", data.Type, "operation", data.Operation.String())
		return nil
	}

	selected := make([]DataMapper, 0, len(data.Mappings))
	for _, dataMapper := range dataMappers {
		if slices.Contains(data.Mappings, dataMapper.Name) {
			selected = append(selected, dataMapper)
		}
	}

	targets := slices.Compact(slices.Sorted(slices.Values(data.Mappings)))
	for _, target := range targets {
		registered := slices.ContainsFunc(dataMappers, func(dataMapper DataMapper) bool { return dataMapper.Name == target })
		if !registered {
			log.Warn("data targets a mapping not registered for its type, ignoring it", "type", data.Type, "mapping", target)
		}
	}

	return selected
}

// applyMapper renders data with dataMapper and forwards the result to the destination.
// A failure is logged and stops only this mapping, so the other mappings registered
// for the same type still run.
func (p *Pipeline) applyMapper(ctx context.Context, data source.Data, dataMapper DataMapper) {
	log := logger.FromContext(ctx).WithName(loggerName)

	log.Trace("sending data", "type", data.Type, "mapping", dataMapper.Name, "operation", data.Operation.String())
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
			log.Error("error applying mapper templates", "type", data.Type, "mapping", dataMapper.Name, "error", err)
			return
		}
		dataToSend.Name = output.Identifier
		if output.Metadata != nil {
			dataToSend.Metadata = output.Metadata
		}
		dataToSend.Data = output.Spec
		if err := p.destination.SendData(ctx, dataToSend); err != nil {
			log.Error("error sending data to destination", "type", data.Type, "mapping", dataMapper.Name, "error", err)
			return
		}
		p.upsertExtraMappedData(ctx, data, extra)
	case source.DataOperationDelete:
		identifier, extra, err := dataMapper.Mapper.ApplyIdentifierTemplate(data.Values)
		dataToSend.Name = identifier
		if err != nil {
			log.Error("error applying mapper templates", "type", data.Type, "mapping", dataMapper.Name, "error", err)
			return
		}
		if err := p.destination.DeleteData(ctx, dataToSend); err != nil {
			log.Error("error deleting data from destination", "type", data.Type, "mapping", dataMapper.Name, "error", err)
			return
		}
		p.deleteExtraMappedData(ctx, data, extra)
	}

	log.Trace("data sent", "type", data.Type, "mapping", dataMapper.Name, "operation", data.Operation.String())
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
