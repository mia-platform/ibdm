// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package pipeline

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/mia-platform/ibdm/internal/destination"
	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/mapper"
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
	Resource   string
	Mapper     mapper.Mapper
}

// Pipeline orchestrates the flow from a source through mappers into a destination.
type Pipeline struct {
	source      any
	mappers     map[string]DataMapper
	mapperTypes []string
	destination destination.Sender
}

// New wires together the given source, mappers, and destination into a Pipeline.
func New(source any, mappers map[string]DataMapper, destination destination.Sender) *Pipeline {
	return &Pipeline{
		source:      source,
		mappers:     mappers,
		mapperTypes: slices.Sorted(maps.Keys(mappers)),
		destination: destination,
	}
}

// Start begins streaming data from a source.EventSource.
func (p *Pipeline) Start(ctx context.Context) error {
	log := logger.FromContext(ctx).WithName(loggerName)

	streamSource, ok := p.source.(source.EventSource)
	if !ok {
		return &unsupportedSourceError{
			Message: "source does not support streaming data",
		}
	}

	log.Trace("starting data pipeline")
	err := p.runDataPipeline(ctx, func(ctx context.Context, channel chan<- source.Data) error {
		return streamSource.StartEventStream(ctx, p.mapperTypes, channel)
	})
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
			mapper, found := p.mappers[data.Type]
			if !found {
				log.Debug("data type not mapped, skipping", "type", data.Type)
				continue
			}

			output, err := mapper.Mapper.ApplyTemplates(data.Values)
			if err != nil {
				log.Error("error applying mapper templates", "type", data.Type, "error", err)
				continue
			}

			log.Trace("sending data", "type", data.Type, "operation", data.Operation.String())
			dataToSend := &destination.Data{
				APIVersion:    mapper.APIVersion,
				Resource:      mapper.Resource,
				Name:          output.Identifier,
				OperationTime: data.Timestamp(),
			}
			switch data.Operation {
			case source.DataOperationUpsert:
				dataToSend.Data = output.Spec
				if err := p.destination.SendData(ctx, dataToSend); err != nil {
					log.Error("error sending data to destination", "type", data.Type, "error", err)
					continue
				}
			case source.DataOperationDelete:
				if err := p.destination.DeleteData(ctx, dataToSend); err != nil {
					log.Error("error deleting data from destination", "type", data.Type, "error", err)
					continue
				}
			}

			log.Trace("data sent", "type", data.Type, "operation", data.Operation.String())
		}
	}
}
