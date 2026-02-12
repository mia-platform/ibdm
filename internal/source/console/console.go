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
	"maps"
	"net/http"
	"slices"
	"strings"
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

	configurationChainTypes = []string{projectResource, revisionResource, serviceResource}
	timeSource              = time.Now
)

type webhookClient struct {
	config webhookConfig
}

var _ source.WebhookSource = &Source{}
var _ source.SyncableSource = &Source{}

type Source struct {
	c  *webhookClient
	cs *service.ConsoleService
}

func NewSource() (*Source, error) {
	consoleClient, err := newConsoleClient()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceCreation, err)
	}

	consoleService, err := service.NewConsoleService()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSourceCreation, err)
	}

	return &Source{
		c:  consoleClient,
		cs: consoleService,
	}, nil
}

func newConsoleClient() (*webhookClient, error) {
	config, err := loadConfigFromEnv()
	if err != nil {
		return nil, err
	}

	return &webhookClient{
		config: *config,
	}, nil
}

func (s *Source) StartSyncProcess(ctx context.Context, typesToSync map[string]source.Extra, results chan<- source.Data) error {
	dataToSync, err := s.listAssets(ctx, typesToSync)
	if err != nil {
		return err
	}

	for _, data := range dataToSync {
		results <- data
	}

	return nil
}

func (s *Source) listAssets(ctx context.Context, typesToSync map[string]source.Extra) ([]source.Data, error) {
	log := logger.FromContext(ctx).WithName(loggerName)

	confChainDataString := make([]string, 0)
	for _, confChainType := range configurationChainTypes {
		if _, found := typesToSync[confChainType]; found {
			confChainDataString = append(confChainDataString, confChainType)
			continue
		}
	}

	if len(confChainDataString) == 0 {
		log.Debug("no known types found, end early")
		return []source.Data{}, nil
	}

	log.Trace("fetching resources needed for configuration chain started", "types", confChainDataString)
	configurationsData, err := s.listConfigurations(ctx, confChainDataString)
	log.Trace("fetching resources needed for configuration chain done", "types", confChainDataString)
	return configurationsData, err
}

//nolint:gocyclo
func (s *Source) listConfigurations(ctx context.Context, subtypes []string) ([]source.Data, error) {
	log := logger.FromContext(ctx).WithName(loggerName)

	dataToSync := make([]source.Data, 0)
	projectList, err := s.cs.GetProjects(ctx)
	syncProjects := slices.Contains(subtypes, projectResource)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRetrievingAssets, err)
	}

	log.Trace("fetched projects", "count", len(projectList))
	for _, project := range projectList {
		if syncProjects {
			data := source.Data{
				Type:      projectResource,
				Operation: source.DataOperationUpsert,
				Time:      timeSource(),
				Values:    map[string]any{"project": project},
			}
			dataToSync = append(dataToSync, data)
		}

		log.Trace("fetching revisions for project", "_id", project["_id"], "projectId", project["projectId"])
		revisions, err := s.cs.GetRevisions(ctx, project["_id"].(string))
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrRetrievingAssets, err.Error())
		}
		log.Trace("fetched revisions", "count", len(revisions), "_id", project["_id"], "projectId", project["projectId"])

		for _, revision := range revisions {
			log.Trace("fetching configuration for project", "_id", project["_id"], "projectId", project["projectId"], "revisionName", revision["name"])

			configuration, err := s.cs.GetConfiguration(ctx, project["_id"].(string), revision["name"].(string))
			if err != nil {
				return nil, fmt.Errorf("%w: %s", ErrRetrievingAssets, err.Error())
			}

			customRemoveFields(configuration)

			for _, typeString := range subtypes {
				switch typeString {
				case revisionResource:
					dataToSync = append(dataToSync, source.Data{
						Type:      revisionResource,
						Operation: source.DataOperationUpsert,
						Time:      timeSource(),
						Values: map[string]any{
							"project": map[string]any{
								"_id":       project["_id"],
								"projectId": project["projectId"],
								"name":      project["name"],
								"tenantId":  project["tenantId"],
							},
							"revision": map[string]any{
								"name": revision["name"],
							},
						},
					})
				case serviceResource:
					if revision["name"].(string) != project["defaultBranch"].(string) {
						continue
					}

					for _, service := range configuration["services"].(map[string]any) {
						svc := service.(map[string]any)
						svcType, typeFound := svc["type"]
						advanced, advancedFound := svc["advanced"]
						if !typeFound || !advancedFound {
							continue
						}

						if svcType.(string) != "custom" || advanced.(bool) {
							continue
						}

						dataToSync = append(dataToSync, source.Data{
							Type:      serviceResource,
							Operation: source.DataOperationUpsert,
							Time:      timeSource(),
							Values: map[string]any{
								"project": map[string]any{
									"_id":       project["_id"],
									"projectId": project["projectId"],
									"name":      project["name"],
									"tenantId":  project["tenantId"],
								},
								"revision": map[string]any{
									"name": revision["name"],
								},
								"service": svc,
							},
						})
					}
				}
			}
		}
	}

	return dataToSync, nil
}

func customRemoveFields(configuration map[string]any) {
	// Remove castFunctions from fastDataConfig to reduce payload size
	if fdConfig, ok := configuration["fastDataConfig"].(map[string]any); ok {
		fdConfig["castFunctions"] = nil
	}

	// Remove castFunctions from fastDataConfig to reduce payload size
	if plugin, ok := configuration["microfrontendPluginsConfig"].(map[string]any); ok {
		if bc, ok := plugin["backofficeConfigurations"].(map[string]any); ok {
			bc["services"] = nil
		}
	}
}

func (s *Source) GetWebhook(ctx context.Context, typesToStream map[string]source.Extra, results chan<- source.Data) (source.Webhook, error) {
	log := logger.FromContext(ctx).WithName(loggerName)

	if err := s.validateWebhookSecret(); err != nil {
		return source.Webhook{}, err
	}

	return source.Webhook{
		Method: http.MethodPost,
		Path:   s.c.config.WebhookPath,
		Handler: func(ctx context.Context, headers http.Header, body []byte) error {
			if !validateSignature(ctx, body, s.c.config.WebhookSecret, headers.Get(authHeaderName)) {
				log.Error("webhook signature validation failed")
				return ErrSignatureMismatch
			}

			var event event
			if err := json.Unmarshal(body, &event); err != nil {
				log.Error(ErrUnmarshalingEvent.Error(), "body", string(body), "error", err.Error())
				return fmt.Errorf("%w: %s", ErrUnmarshalingEvent, err.Error())
			}

			if !event.IsTypeIn(slices.Sorted(maps.Keys(typesToStream))) {
				log.Debug("ignoring event with unlisted type", "eventName ", event.EventName, "resource", event.GetResource())
				return nil
			}

			log.Trace("received event", "type", event.EventName, "resource", event.GetResource(), "payload", event.Payload, "timestamp", event.UnixEventTimestamp())

			go func(ctx context.Context) {
				if err := doChain(ctx, event, results, s.cs); err != nil {
					log.Error("error processing event chain", "error", err.Error())
				}
			}(ctx)
			return nil
		},
	}, nil
}

func (s *Source) validateWebhookSecret() error {
	if s.c.config.WebhookSecret == "" {
		return ErrWebhookSecretMissing
	}
	return nil
}

func doChain(ctx context.Context, event event, channel chan<- source.Data, cs *service.ConsoleService) error {
	var data *source.Data
	var err error
	switch event.GetResource() {
	case configurationResource:
		data, err = configurationEventChain(ctx, event, cs)
	case projectResource:
		data = defaultEventChain(event)
	default:
		data = defaultEventChain(event)
	}
	if err != nil {
		return fmt.Errorf("%w: %s", ErrEventChainProcessing, err.Error())
	}
	channel <- *data
	return nil
}

func defaultEventChain(event event) *source.Data {
	return &source.Data{
		Type:      event.GetResource(),
		Operation: event.Operation(),
		Values:    event.Payload,
		Time:      event.UnixEventTimestamp(),
	}
}

func configurationEventChain(ctx context.Context, event event, cs *service.ConsoleService) (*source.Data, error) {
	log := logger.FromContext(ctx).WithName(loggerName)

	var projectID, revisionName, tenantID string
	var ok bool
	if event.Payload == nil {
		return nil, errors.New("configuration event payload is nil")
	}
	if projectID, ok = event.Payload["projectId"].(string); !ok {
		return nil, errors.New("configuration event payload missing projectId")
	}
	if revisionName, ok = event.Payload["revisionName"].(string); !ok {
		return nil, errors.New("configuration event payload missing revisionName")
	}
	if tenantID, ok = event.Payload["tenantId"].(string); !ok {
		log.Error("configuration event payload missing tenantId")
	}

	configuration, err := getProjectConfiguration(ctx, tenantID, projectID, revisionName, cs)
	if err != nil {
		return nil, err
	}

	data := source.Data{
		Type:      configurationResource,
		Operation: source.DataOperationUpsert,
		Values:    configuration,
		Time:      event.UnixEventTimestamp(),
	}

	return &data, nil
}

func getProjectConfiguration(ctx context.Context, tenantID, projectID, revisionName string, cs *service.ConsoleService) (map[string]any, error) {
	log := logger.FromContext(ctx).WithName(loggerName)

	log.Trace("fetching full project", "_id", projectID)
	project, err := cs.GetProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRetrievingAssets, err.Error())
	}

	log.Trace("fetching configuration for project", "_id", projectID, "revisionName", revisionName)
	configuration, err := cs.GetConfiguration(ctx, projectID, revisionName)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRetrievingAssets, err.Error())
	}

	customRemoveFields(configuration)

	configurationData := map[string]any{
		"project": map[string]any{
			"_id":       projectID,
			"projectId": project["projectId"],
			"name":      project["name"],
			"tenantId":  tenantID,
		},
		"revision": map[string]any{
			"name": revisionName,
		},
		"configuration": configuration,
	}

	return configurationData, nil
}

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
