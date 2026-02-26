// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package sysdig

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
)

const (
	loggerName = "ibdm:source:sysdig"

	// vulnerabilityType is the only data type supported by the Sysdig source.
	vulnerabilityType = "vulnerability"

	// sysqlVulnerabilityQuery is the hardcoded SysQL query for fetching all
	// image vulnerabilities. A future version may introduce user-configurable
	// queries if filtering or additional entity types are needed.
	sysqlVulnerabilityQuery = "MATCH Image AS img AFFECTED_BY Vulnerability AS vuln RETURN img, vuln"
)

var (
	// ErrSysdigSource wraps all errors originating from the Sysdig source.
	ErrSysdigSource = errors.New("sysdig source")

	// knownTypes maps supported data type keys to their SysQL queries.
	knownTypes = map[string]string{
		vulnerabilityType: sysqlVulnerabilityQuery,
	}

	// timeSource is a package-level function for the current time, replaceable in tests.
	timeSource = time.Now
)

var _ source.SyncableSource = &Source{}

// Source implements [source.SyncableSource] for Sysdig Secure. It queries the
// SysQL API to fetch vulnerability data and pushes results through the IBDM
// pipeline.
type Source struct {
	config config
	client *http.Client

	syncLock sync.Mutex
}

// NewSource constructs a [Source] by reading its configuration from environment
// variables and initialising the underlying HTTP client. It returns
// [ErrSysdigSource] if the configuration is invalid.
func NewSource() (*Source, error) {
	cfg, err := loadConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSysdigSource, err)
	}

	return &Source{
		config: *cfg,
		client: &http.Client{
			Timeout: cfg.HTTPTimeout,
		},
	}, nil
}

// StartSyncProcess performs a full synchronisation of the requested resource
// types by querying the Sysdig SysQL API and sending results to results.
// Only known data types are processed; unknown types are skipped with a debug
// log message.
func (s *Source) StartSyncProcess(ctx context.Context, typesToSync map[string]source.Extra, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	if !s.syncLock.TryLock() {
		log.Debug("sync process already running")
		return nil
	}
	defer s.syncLock.Unlock()

	for dataType := range typesToSync {
		if err := ctx.Err(); err != nil {
			return nil
		}

		query, ok := knownTypes[dataType]
		if !ok {
			log.Debug("skipping unknown data type", "type", dataType)
			continue
		}

		log.Trace("starting sync for data type", "type", dataType)

		if err := s.syncType(ctx, dataType, query, results); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			log.Error("error syncing data type", "type", dataType, "error", err.Error())
			continue
		}

		log.Trace("completed sync for data type", "type", dataType)
	}

	return nil
}

// syncType fetches all pages for a single data type and pushes each result
// row onto the results channel.
func (s *Source) syncType(ctx context.Context, dataType, query string, results chan<- source.Data) error {
	return queryAllPages(ctx, s.client, s.config.URL, s.config.APIToken, query, s.config.PageSize, func(items []map[string]any) error {
		for _, item := range items {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case results <- source.Data{
				Type:      dataType,
				Operation: source.DataOperationUpsert,
				Values:    item,
				Time:      timeSource(),
			}:
			}
		}
		return nil
	})
}
