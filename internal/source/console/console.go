// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mia-platform/ibdm/internal/logger"
	"github.com/mia-platform/ibdm/internal/source"
	"github.com/mia-platform/ibdm/internal/source/console/service"
)

const (
	loggerName            = "ibdm:source:console"
	authHeaderName        = "X-Mia-Signature"
	configurationResource = "configuration"
	projectResource       = "project"
	revisionResource      = "revision"
	serviceResource       = "service"
)

var (
	ErrSourceCreation       = errors.New("source creation error")
	ErrUnmarshalingEvent    = errors.New("error unmarshaling event")
	ErrEventChainProcessing = errors.New("error in event processing chain")
	ErrSignatureMismatch    = errors.New("webhook signature mismatch")
	ErrRetrievingAssets     = errors.New("error retrieving assets")
	ErrWebhookSecretMissing = errors.New("webhook secret not configured")

	configurationChainTypes       = []string{projectResource, revisionResource, serviceResource}
	configurationChainTypesStream = []string{revisionResource, serviceResource}
	timeSource                    = time.Now
)

// webhookClient wraps the webhook configuration needed to receive and
// validate incoming Console webhook requests.
type webhookClient struct {
	config webhookConfig
}

var _ source.WebhookSource = &Source{}
var _ source.SyncableSource = &Source{}

// Source implements [source.WebhookSource] and [source.SyncableSource] for the
// Mia Platform Console. It can both poll all assets via the Console API and
// receive real-time mutations through a signed webhook.
type Source struct {
	c  *webhookClient
	cs *service.ConsoleService

	syncLock sync.Mutex
}

// NewSource constructs a [Source] by reading its configuration from environment
// variables and initialising the underlying Console API client. It returns
// [ErrSourceCreation] if either the webhook config or the API client cannot be
// created.
func NewSource() (*Source, error) {
	config, err := loadConfigFromEnv()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceCreation, err)
	}

	cs, err := service.NewConsoleService()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceCreation, err)
	}

	return &Source{
		c:  &webhookClient{config: *config},
		cs: cs,
	}, nil
}

// StartSyncProcess performs a full synchronisation of the requested resource
// types by listing all matching assets from the Console API and sending them to
// results. It blocks until every item has been written to the channel.
func (s *Source) StartSyncProcess(ctx context.Context, typesToSync map[string]source.Extra, results chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)
	if !s.syncLock.TryLock() {
		log.Debug("sync process already running")
		return nil
	}

	dataToSync, err := s.listAssets(ctx, typesToSync)
	if err != nil {
		return err
	}

	for _, data := range dataToSync {
		results <- data
	}

	return nil
}

// filterTypes returns the subset of candidates that are present as keys in
// types, preserving the original ordering of candidates.
func filterTypes(candidates []string, types map[string]source.Extra) []string {
	var result []string
	for _, t := range candidates {
		if _, ok := types[t]; ok {
			result = append(result, t)
		}
	}
	return result
}

// listAssets resolves the requested typesToSync against the known configuration
// chain types and delegates to listConfigurations. It returns early with an
// empty slice when none of the requested types are relevant.
func (s *Source) listAssets(ctx context.Context, typesToSync map[string]source.Extra) ([]source.Data, error) {
	log := logger.FromContext(ctx).WithName(loggerName)

	subtypes := filterTypes(configurationChainTypes, typesToSync)
	if len(subtypes) == 0 {
		log.Debug("no known types found, end early")
		return []source.Data{}, nil
	}

	log.Trace("fetching resources needed for configuration chain started", "types", subtypes)
	data, err := s.listConfigurations(ctx, subtypes)
	log.Trace("fetching resources needed for configuration chain done", "types", subtypes)
	return data, err
}

// listConfigurations fetches all projects from the Console API and, for each
// project, retrieves its revisions and configurations. It emits [source.Data]
// entries for every requested subtype (project, revision, service). Services
// are only emitted for the project's default branch.
func (s *Source) listConfigurations(ctx context.Context, subtypes []string) ([]source.Data, error) {
	log := logger.FromContext(ctx).WithName(loggerName)

	syncProjects := slices.Contains(subtypes, projectResource)
	projects, err := s.cs.GetProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
	}

	var result []source.Data
	log.Trace("fetched projects", "count", len(projects))

	for _, project := range projects {
		if syncProjects {
			result = append(result, source.Data{
				Type:      projectResource,
				Operation: source.DataOperationUpsert,
				Time:      timeSource(),
				Values:    map[string]any{"project": project},
			})
		}

		log.Trace("fetching revisions for project", "_id", project["_id"], "projectId", project["projectId"])
		revisions, err := s.cs.GetRevisions(ctx, project["_id"].(string))
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
		}
		log.Trace("fetched revisions", "count", len(revisions), "_id", project["_id"], "projectId", project["projectId"])

		for _, revision := range revisions {
			revName := revision["name"].(string)
			log.Trace("fetching configuration for project", "_id", project["_id"], "projectId", project["projectId"], "revisionName", revName)

			configuration, err := s.cs.GetConfiguration(ctx, project["_id"].(string), revName)
			if err != nil {
				return nil, fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
			}

			customRemoveFields(configuration)

			for _, typeString := range subtypes {
				switch typeString {
				case revisionResource:
					result = append(result, createRevisionData(project, revName, timeSource()))
				case serviceResource:
					if revName != project["defaultBranch"].(string) {
						continue
					}
					for _, svc := range configuration["services"].(map[string]any) {
						svcMap := svc.(map[string]any)
						if isServiceValid(svcMap) {
							result = append(result, createServiceData(project, revName, svcMap, timeSource()))
						}
					}
				}
			}
		}
	}

	return result, nil
}

// customRemoveFields strips heavy or irrelevant fields from a raw
// configuration map in-place to reduce downstream payload size.
func customRemoveFields(configuration map[string]any) {
	if fdConfig, ok := configuration["fastDataConfig"].(map[string]any); ok {
		fdConfig["castFunctions"] = nil
	}
	if plugin, ok := configuration["microfrontendPluginsConfig"].(map[string]any); ok {
		if bc, ok := plugin["backofficeConfigurations"].(map[string]any); ok {
			bc["services"] = nil
		}
	}
}

// buildProjectData returns a normalised project map containing only the
// canonical identifier fields (_id, projectId, name, tenantId).
func buildProjectData(project map[string]any) map[string]any {
	return map[string]any{
		"_id":       project["_id"],
		"projectId": project["projectId"],
		"name":      project["name"],
		"tenantId":  project["tenantId"],
		"info":      project["info"],
	}
}

// buildRevisionData returns a minimal revision map keyed by "name".
func buildRevisionData(revisionName string) map[string]any {
	return map[string]any{"name": revisionName}
}

// isServiceValid reports whether svc qualifies for synchronisation. A service
// is valid when its type is "custom" and it is not marked as advanced.
func isServiceValid(svc map[string]any) bool {
	svcType, typeFound := svc["type"]
	advanced, advancedFound := svc["advanced"]
	return typeFound && advancedFound && svcType.(string) == "custom" && !advanced.(bool)
}

// createRevisionData assembles a [source.Data] upsert entry for a revision,
// embedding the normalised project and revision maps.
func createRevisionData(project map[string]any, revisionName string, t time.Time) source.Data {
	return source.Data{
		Type:      revisionResource,
		Operation: source.DataOperationUpsert,
		Time:      t,
		Values: map[string]any{
			"project":  buildProjectData(project),
			"revision": buildRevisionData(revisionName),
		},
	}
}

// createServiceData assembles a [source.Data] upsert entry for a service,
// embedding the normalised project, revision and service maps.
func createServiceData(project map[string]any, revisionName string, svc map[string]any, t time.Time) source.Data {
	return source.Data{
		Type:      serviceResource,
		Operation: source.DataOperationUpsert,
		Time:      t,
		Values: map[string]any{
			"project":  buildProjectData(project),
			"revision": buildRevisionData(revisionName),
			"service":  svc,
		},
	}
}

// GetWebhook returns a [source.Webhook] that validates incoming Console
// webhook requests against a shared secret and dispatches matching events to
// results asynchronously. It returns [ErrWebhookSecretMissing] when no secret
// is configured.
func (s *Source) GetWebhook(ctx context.Context, typesToStream map[string]source.Extra, results chan<- source.Data) (source.Webhook, error) {
	log := logger.FromContext(ctx).WithName(loggerName)

	if s.c.config.WebhookSecret == "" {
		return source.Webhook{}, ErrWebhookSecretMissing
	}

	var webhookTypes []string
	for t := range typesToStream {
		switch t {
		case projectResource:
			webhookTypes = append(webhookTypes, t)
		case revisionResource, serviceResource:
			webhookTypes = append(webhookTypes, configurationResource)
		}
	}

	return source.Webhook{
		Method: http.MethodPost,
		Path:   s.c.config.WebhookPath,
		Handler: func(ctx context.Context, headers http.Header, body []byte) error {
			if !validateSignature(ctx, body, s.c.config.WebhookSecret, headers.Get(authHeaderName)) {
				log.Error("webhook signature validation failed")
				return ErrSignatureMismatch
			}

			var ev event
			if err := json.Unmarshal(body, &ev); err != nil {
				log.Error(ErrUnmarshalingEvent.Error(), "body", string(body), "error", err.Error())
				return fmt.Errorf("%w: %w", ErrUnmarshalingEvent, err)
			}

			if !ev.IsTypeIn(webhookTypes) {
				log.Debug("ignoring event with unlisted type", "eventName ", ev.EventName, "resource", ev.GetResource())
				return nil
			}

			log.Trace("received event", "type", ev.EventName, "resource", ev.GetResource(), "payload", ev.Payload, "timestamp", ev.UnixEventTimestamp())

			go func(ctx context.Context) {
				if err := s.handleEvent(ctx, ev, typesToStream, results); err != nil {
					log.Error("error processing event chain", "error", err.Error())
				}
			}(ctx)
			return nil
		},
	}, nil
}

// handleEvent routes an incoming event to the appropriate processing path.
// Configuration events are expanded via configurationEventChain; all other
// events are forwarded to channel as-is.
func (s *Source) handleEvent(ctx context.Context, ev event, types map[string]source.Extra, channel chan<- source.Data) error {
	switch ev.GetResource() {
	case configurationResource:
		subtypes := filterTypes(configurationChainTypesStream, types)
		if err := s.configurationEventChain(ctx, ev, subtypes, channel); err != nil {
			return fmt.Errorf("%w: %w", ErrEventChainProcessing, err)
		}
	default:
		channel <- source.Data{
			Type:      ev.GetResource(),
			Operation: ev.Operation(),
			Values:    ev.Payload,
			Time:      ev.UnixEventTimestamp(),
		}
	}
	return nil
}

// configurationEventChain enriches a configuration webhook event by fetching
// the full project from the Console API and emitting revision and/or service
// data entries to channel based on the requested types.
func (s *Source) configurationEventChain(ctx context.Context, ev event, types []string, channel chan<- source.Data) error {
	log := logger.FromContext(ctx).WithName(loggerName)

	if ev.Payload == nil {
		return errors.New("configuration event payload is nil")
	}
	projectID, ok := ev.Payload["projectId"].(string)
	if !ok {
		return errors.New("configuration event payload missing projectId")
	}
	revisionName, ok := ev.Payload["revisionName"].(string)
	if !ok {
		return errors.New("configuration event payload missing revisionName")
	}

	log.Trace("fetching full project", "_id", projectID)
	project, err := s.cs.GetProject(ctx, projectID)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
	}

	for _, t := range types {
		switch t {
		case revisionResource:
			channel <- createRevisionData(project, revisionName, ev.UnixEventTimestamp())
		case serviceResource:
			if revisionName != project["defaultBranch"].(string) {
				continue
			}

			configuration, err := s.cs.GetConfiguration(ctx, projectID, revisionName)
			if err != nil {
				return fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
			}

			for _, svc := range configuration["services"].(map[string]any) {
				svcMap := svc.(map[string]any)
				if isServiceValid(svcMap) {
					channel <- createServiceData(project, revisionName, svcMap, ev.UnixEventTimestamp())
				}
			}
		}
	}

	return nil
}

// validateSignature verifies a Console webhook request by computing
// SHA-256(body || secret) and comparing the result with the hex-encoded
// signature from then header (with an optional "sha256=" prefix stripped).
func validateSignature(ctx context.Context, body []byte, secret, signatureHeader string) bool {
	log := logger.FromContext(ctx).WithName(loggerName)

	signature, _ := strings.CutPrefix(signatureHeader, "sha256=")

	hasher := sha256.New()
	hasher.Write(body)
	hasher.Write([]byte(secret))
	generatedMAC := hasher.Sum(nil)

	expectedMac, err := hex.DecodeString(signature)
	if err != nil {
		log.Error("error decoding webhook signature", "error", err.Error())
		return false
	}

	return hmac.Equal(generatedMAC, expectedMac)
}
