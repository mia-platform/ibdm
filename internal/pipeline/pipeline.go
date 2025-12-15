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

// dataPipeline defines a function type that represents a data processing function
// which takes a context and a channel to send source.Data, returning an error if any.
type dataPipeline = func(ctx context.Context, channel chan<- source.Data) error

type DataMapper struct {
	APIVersion string
	Resource   string
	Mapper     mapper.Mapper
}

// Pipeline represents a data processing pipeline that reads data from a source,
// applies mappers to transform the data, and sends the processed data to a destination.
type Pipeline struct {
	source      any
	mappers     map[string]DataMapper
	mapperTypes []string
	destination destination.Sender
}

// New creates a new Pipeline instance with the provided source, mappers, and destination.
func New(source any, mappers map[string]DataMapper, destination destination.Sender) *Pipeline {
	return &Pipeline{
		source:      source,
		mappers:     mappers,
		mapperTypes: slices.Sorted(maps.Keys(mappers)),
		destination: destination,
	}
}

// Start initiates the data processing pipeline by starting the event stream from the source.
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

// Sync performs a one-time synchronization of data from the source to the destination.
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

// runDataPipeline orchestrates the data processing by starting the mapping process
// and executing the provided dataPipeline function.
// It ensures that the mapping process completes before returning.
func (p *Pipeline) runDataPipeline(ctx context.Context, dataPipeline dataPipeline) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	channel := make(chan source.Data)

	// use channel to signal when the mapping stream has exhausted all the queue messages
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

// Stop attempts to gracefully stop the source if it implements the ClosableSource interface.
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

// mappingData reads data from the provided channel, applies the appropriate mapper based on the
// data type, and sends the processed data to the destination.
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
				APIVersion: mapper.APIVersion,
				Resource:   mapper.Resource,
				Name:       output.Identifier,
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
