// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package functions

import (
	"fmt"
	"time"
)

var nowFn = time.Now

// Now returns the current UTC time formatted as RFC3339.
func Now() string {
	return nowFn().UTC().Format(time.RFC3339)
}

// ConvertFromTimestamp converts a Unix timestamp (seconds since epoch) to a UTC
// time string formatted as RFC3339.
func ConvertFromTimestamp(v any) (string, error) {
	// Note: Data decoded from JSON always produces float64; int64 is accepted as a
	// convenience for values set explicitly in source code. The fractional part of
	// float64 values is truncated. It returns an error for unsupported types.

	var sec int64
	switch ts := v.(type) {
	case float64:
		sec = int64(ts)
	case int64:
		sec = ts
	default:
		return "", fmt.Errorf("cannot convert type %T to timestamp", v)
	}

	return time.Unix(sec, 0).UTC().Format(time.RFC3339), nil
}
