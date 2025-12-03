// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package pipeline

import (
	"context"

	"github.com/mia-platform/ibdm/internal/mapper"
)

type DataDestination interface {
	SendData(ctx context.Context, data mapper.MappedData) error
	DeleteData(ctx context.Context, identifier string) error
}
