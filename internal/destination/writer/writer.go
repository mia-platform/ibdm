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

// NewDestination returns a destination that prints payloads to w.
func NewDestination(w io.Writer) destination.Sender {
	return &writerDestination{
		writer: w,
	}
}

// SendData renders the upsert payload as formatted JSON and writes it to the destination writer.
func (d *writerDestination) SendData(_ context.Context, data *destination.Data) error {
	builder := new(strings.Builder)

	builder.WriteString("Send data:\n")
	builder.WriteString("\tAPIVersion: " + data.APIVersion + "\n")
	builder.WriteString("\tItemFamily: " + data.ItemFamily + "\n")
	builder.WriteString("\tItem Name: " + data.Name + "\n")
	builder.WriteString("\tTimestamp: " + data.OperationTime + "\n")
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

// DeleteData renders the delete payload and writes it to the destination writer.
func (d *writerDestination) DeleteData(_ context.Context, data *destination.Data) error {
	builder := new(strings.Builder)
	builder.WriteString("Delete data:\n")
	builder.WriteString("\tAPIVersion: " + data.APIVersion + "\n")
	builder.WriteString("\tItemFamily: " + data.ItemFamily + "\n")
	builder.WriteString("\tItem Name: " + data.Name + "\n")
	builder.WriteString("\tTimestamp: " + data.OperationTime + "\n")
	builder.WriteString("\n")

	d.lock.Lock()
	defer d.lock.Unlock()
	fmt.Fprint(d.writer, builder.String())
	return nil
}
