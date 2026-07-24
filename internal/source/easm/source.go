// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package easm

import (
	"errors"
	"sync"
	"time"

	"github.com/mia-platform/ibdm/internal/source"
)

const (
	loggerName = "ibdm:source:easm"

	domainType        = "domain"
	hostType          = "host"
	ipType            = "ip"
	endpointType      = "endpoint"
	vulnerabilityType = "vulnerability"
)

// knownTypes is the set of item types the source can emit. The endpoint tags
// each item with one of these; anything else is skipped.
var knownTypes = map[string]struct{}{
	domainType:        {},
	hostType:          {},
	ipType:            {},
	endpointType:      {},
	vulnerabilityType: {},
}

var (
	// ErrEASMSource wraps errors emitted by the EASM source implementation.
	ErrEASMSource = errors.New("easm source")

	// timeSource is a replaceable function for obtaining the current time.
	// Tests override this to produce deterministic timestamps.
	timeSource = time.Now
)

var _ source.SyncableSource = &Source{}

// Source implements source.SyncableSource for our EASM scan results.
type Source struct {
	config config
	client *client

	syncLock sync.Mutex
}

// NewSource creates a new EASM Source reading configuration from environment variables.
func NewSource() (*Source, error) {
	cfg, err := loadConfigFromEnv()
	if err != nil {
		return nil, handleErr(err)
	}

	c, err := newClient(cfg)
	if err != nil {
		return nil, handleErr(err)
	}

	return &Source{
		config: cfg,
		client: c,
	}, nil
}
