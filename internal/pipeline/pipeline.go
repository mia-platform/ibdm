// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package pipeline

import (
	"context"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/mapper"
)

const (
	loggerName = "ibdm:pipeline"
)

type Data struct {
	Type string
	Data map[string]any
}

type Pipeline struct {
	mappers     map[string]mapper.Mapper
	dataChannel <-chan Data
	destination Destination
}

type Destination interface {
	Write(ctx context.Context, data mapper.MappedData) (err error)
}

func New(dataChannel <-chan Data, mappers map[string]mapper.Mapper, destination Destination) *Pipeline {
	return &Pipeline{
		mappers:     mappers,
		dataChannel: dataChannel,
		destination: destination,
	}
}

func (p *Pipeline) Run(ctx context.Context) {
	log := logger.FromContext(ctx).WithName(loggerName)
	log.Info("starting pipeline")

	for {
		select {
		case <-ctx.Done():
			err := ctx.Err()
			switch err {
			case context.Canceled:
				log.Info("cancel signal received, stopping pipeline")
				return
			case context.DeadlineExceeded:
				log.Info("timeout exceeded, stopping pipeline")
				return
			}
		case data, ok := <-p.dataChannel:
			if !ok {
				log.Info("data channel closed, stopping pipeline")
				return
			}

			p.consumeData(ctx, data)
		}
	}
}

func (p *Pipeline) consumeData(ctx context.Context, data Data) {
	log := logger.FromContext(ctx).WithName(loggerName)
	mapper, exists := p.mappers[data.Type]
	if !exists {
		log.Trace("data discarded for missing template", "type", data.Type)
		return
	}

	mappedData, err := mapper.ApplyTemplates(data.Data)
	if err != nil {
		log.Error("error mapping data", "error", err, "type", data.Type)
		return
	}

	if err := p.destination.Write(ctx, mappedData); err != nil {
		log.Error("error writing data to destination", "error", err, "type", data.Type)
		return
	}
}
