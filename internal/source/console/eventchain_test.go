// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mia-platform/ibdm/internal/source"
)

func TestEventChain_DoChain(t *testing.T) {
	tests := map[string]struct {
		event         event
		expectedError error
		expectedData  []source.Data
	}{
		"configuration event": {
			event: event{
				EventName:      "configuration_created",
				EventTimestamp: 1672531200, // 2023-01-01 00:00:00 UTC
				Payload: map[string]any{
					"key": "value",
				},
			},
			expectedData: []source.Data{
				{
					Type:      "configuration",
					Operation: source.DataOperationUpsert,
					Values: map[string]any{
						"key": "value",
					},
					Time: time.Unix(1672531200, 0),
				},
			},
		},
		"project event: delete": {
			event: event{
				EventName:      "project_deleted",
				EventTimestamp: 1672531200,
				Payload: map[string]any{
					"id": "123",
				},
			},
			expectedData: []source.Data{
				{
					Type:      "project",
					Operation: source.DataOperationDelete,
					Values: map[string]any{
						"id": "123",
					},
					Time: time.Unix(1672531200, 0),
				},
			},
		},
		"other event": {
			event: event{
				EventName:      "other_resource_updated",
				EventTimestamp: 1672531200,
				Payload: map[string]any{
					"foo": "bar",
				},
			},
			expectedData: []source.Data{
				{
					Type:      "other_resource",
					Operation: source.DataOperationUpsert,
					Values: map[string]any{
						"foo": "bar",
					},
					Time: time.Unix(1672531200, 0),
				},
			},
		},
	}

	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			ec := &eventChain{event: test.event}
			ch := make(chan source.Data, len(test.expectedData)+1)

			err := ec.doChain(ch)
			if test.expectedError != nil {
				require.ErrorIs(t, err, test.expectedError)
				return
			}
			require.NoError(t, err)
			close(ch)

			var data []source.Data
			for d := range ch {
				data = append(data, d)
			}
			require.ElementsMatch(t, test.expectedData, data)
		})
	}
}
