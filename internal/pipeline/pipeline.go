// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package pipeline

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/mapper"
)

const (
	loggerName = "ibdm:pipeline"
)

type Pipeline struct {
	source      any
	mappers     map[string]mapper.Mapper
	destination DataDestination
}

func New(source any, mappers map[string]mapper.Mapper, destination DataDestination) *Pipeline {
	return &Pipeline{
		source:      source,
		mappers:     mappers,
		destination: destination,
	}
}

func (p *Pipeline) Start(ctx context.Context) error {
	log := logger.FromContext(ctx).WithName(loggerName)

	streamSource, ok := p.source.(EventSource)
	if !ok {
		return &unsupportedSourceError{
			Message: "source does not support streaming data",
		}
	}

	log.Trace("starting data pipeline")
	channel := make(chan SourceData)

	// use channel to signal when the mapping stream has exhausted all the queue messages
	mappingDone := make(chan struct{})
	go func() {
		log.Trace("starting data mapping goroutine")
		p.mappingData(ctx, channel)
		mappingDone <- struct{}{}
	}()

	err := streamSource.StartEventStream(ctx, slices.Sorted(maps.Keys(p.mappers)), channel)
	log.Trace("event stream finished, closing data channel")
	close(channel)

	<-mappingDone
	log.Trace("data mapping goroutine finished")
	return err
}

func (p *Pipeline) Stop(ctx context.Context, timeout time.Duration) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	closableSource, ok := p.source.(ClosableSource)
	if !ok {
		log.Debug("source does not implement ClosableSource, skipping close")
		return nil
	}

	log.Debug("stop source")
	return closableSource.Close(ctx, timeout)
}

func (p *Pipeline) mappingData(ctx context.Context, channel <-chan SourceData) {
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
			mapper := p.mappers[data.Type]
			if mapper == nil {
				log.Debug("data type not mapped, skipping", "type", data.Type)
				continue
			}

			output, err := mapper.ApplyTemplates(data.Values)
			if err != nil {
				log.Error("error applying mapper templates", "type", data.Type, "error", err)
				continue
			}

			log.Debug("sending data", "type", data.Type, "operation", data.Operation)
			switch data.Operation {
			case DataOperationUpsert:
				if err := p.destination.SendData(ctx, output); err != nil {
					log.Error("error sending data to destination", "type", data.Type, "error", err)
					continue
				}
			case DataOperationDelete:
				if err := p.destination.DeleteData(ctx, output.Identifier); err != nil {
					log.Error("error deleting data from destination", "type", data.Type, "error", err)
					continue
				}
			}

			log.Debug("data sent", "type", data.Type, "operation", data.Operation)
		}
	}
}
