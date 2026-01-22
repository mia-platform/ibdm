// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import (
	"errors"
	"fmt"

	"github.com/mia-platform/ibdm/internal/source"
)

var (
	ErrEventChainProcessing = errors.New("error in event processing chain")
)

type eventChain struct {
	event event
}

func (ec *eventChain) doChain(channel chan<- source.Data) error {
	var data []source.Data
	var err error
	switch ec.event.GetResource() {
	case "configuration":
		data, err = configurationEventChain(ec.event)
	case "project":
		data = defaultEventChain(ec.event)
	default:
		data = defaultEventChain(ec.event)
	}
	if err != nil {
		return fmt.Errorf("%w: %s", ErrEventChainProcessing, err.Error())
	}
	for _, d := range data {
		channel <- d
	}
	return nil
}

func defaultEventChain(event event) []source.Data {
	return []source.Data{
		{
			Type:      event.GetResource(),
			Operation: event.Operation(),
			Values:    event.Payload,
			Time:      event.UnixEventTimestamp(),
		},
	}
}

func configurationEventChain(event event) ([]source.Data, error) {
	return []source.Data{
		{
			Type:      event.GetResource(),
			Operation: event.Operation(),
			Values:    event.Payload,
			Time:      event.UnixEventTimestamp(),
		},
	}, nil
}
