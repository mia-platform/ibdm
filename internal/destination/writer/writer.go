// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package writer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/mia-platform/ibdm/internal/destination"
)

var _ destination.Sender = &writerDestination{}

type writerDestination struct {
	writer io.Writer

	lock sync.Mutex
}

func NewDestination(w io.Writer) destination.Sender {
	return &writerDestination{
		writer: w,
	}
}

func (d *writerDestination) SendData(_ context.Context, data *destination.Data) error {
	builder := new(strings.Builder)

	builder.WriteString("Send data:\n")
	builder.WriteString("\tAPIVersion: " + data.APIVersion + "\n")
	builder.WriteString("\tResource: " + data.Resource + "\n")
	builder.WriteString("\tResource Name: " + data.Name + "\n")
	builder.WriteString("\tSpec: ")

	encoder := json.NewEncoder(builder)
	encoder.SetIndent("\t", "\t")
	_ = encoder.Encode(data.Data)
	builder.WriteString("\n")

	d.lock.Lock()
	defer d.lock.Unlock()
	fmt.Fprint(d.writer, builder.String())
	return nil
}

func (d *writerDestination) DeleteData(_ context.Context, data *destination.Data) error {
	builder := new(strings.Builder)
	builder.WriteString("Delete data:\n")
	builder.WriteString("\tAPIVersion: " + data.APIVersion + "\n")
	builder.WriteString("\tResource: " + data.Resource + "\n")
	builder.WriteString("\tResource Name: " + data.Name + "\n")
	builder.WriteString("\n")

	d.lock.Lock()
	defer d.lock.Unlock()
	fmt.Fprint(d.writer, builder.String())
	return nil
}
