// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package easm

import (
	"context"
	"errors"
	"fmt"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

// StartSyncProcess implements source.SyncableSource. It reads the customer's
// latest completed run from the /data endpoint as a single cursor-paginated
// list and emits one source.Data per item, routed by the item's own "type"
// field. Filtering by typesToSync lets the pipeline sync a subset of types.
func (s *Source) StartSyncProcess(ctx context.Context, typesToSync map[string]source.Extra, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)

	if !s.syncLock.TryLock() {
		log.Debug("sync process already running")
		return nil
	}
	defer s.syncLock.Unlock()

	// Log unknown requested types.
	for typeKey := range typesToSync {
		if _, ok := knownTypes[typeKey]; !ok {
			log.Debug("unknown type requested, skipping", "type", typeKey)
		}
	}

	cursor := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		log.Trace("fetching data page", "cursor", cursor)

		page, err := s.client.fetchDataPage(ctx, cursor)
		if err != nil {
			return handleErr(err)
		}

		for _, item := range page.items {
			itemType, ok := item["type"].(string)
			if !ok || itemType == "" {
				log.Debug("item without a type, skipping", "id", item["id"])
				continue
			}

			if _, requested := typesToSync[itemType]; !requested {
				continue
			}

			results <- source.Data{
				Type:      itemType,
				Operation: source.DataOperationUpsert,
				Values:    item,
				Time:      timeSource(),
			}
		}

		if page.nextCursor == "" {
			break
		}
		cursor = page.nextCursor
	}

	return nil
}

// handleErr wraps non-nil errors with ErrEASMSource, matching the project convention.
// Context cancellation errors are silently swallowed (return nil).
func handleErr(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) {
		return nil
	}

	return fmt.Errorf("%w: %w", ErrEASMSource, err)
}
