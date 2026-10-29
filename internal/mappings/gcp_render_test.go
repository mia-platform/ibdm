// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package mappings

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/mia-platform/ibdm/internal/config"
	"github.com/mia-platform/ibdm/internal/mapper"
)

const (
	gcpSource      = "gcp"
	gcpFixturesDir = "testdata/gcp"
)

// loadGCPMapper builds the mapper of the bundled GCP mapping called name, like the CLI does.
func loadGCPMapper(t *testing.T, name string) (mapper.Mapper, *config.MappingConfig) {
	t.Helper()

	configs, err := ForSource(gcpSource)
	require.NoError(t, err)

	for _, mapping := range configs {
		if mapping.Name != name {
			continue
		}

		templates := mapping.Mappings
		m, err := mapper.New(templates.Identifier, templates.Metadata, templates.Spec, templates.Extra, mapper.WithCreateIf(mapping.CreateIf))
		require.NoError(t, err)
		return m, mapping
	}

	require.FailNow(t, "bundled gcp mapping not found", name)
	return nil, nil
}

// loadGCPFixture reads a Cloud Asset payload fixture the way the source emits it: a generic map.
func loadGCPFixture(t *testing.T, name string) map[string]any {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(gcpFixturesDir, name))
	require.NoError(t, err)

	var asset map[string]any
	require.NoError(t, yaml.Unmarshal(content, &asset))
	return asset
}

const (
	gcpURNPrefix        = "urn:mia-platform-catalog:gcp.mia-platform.eu:v1:"
	relationshipPartOf  = "urn:mia-platform-catalog:mia-platform.eu:v1:RelationshipType:part-of.mia-platform.eu"
	relationshipDepends = "urn:mia-platform-catalog:mia-platform.eu:v1:RelationshipType:dependency.mia-platform.eu"
)

// gcpURN returns the Catalog URN of the item of kind with identifier.
func gcpURN(kind, identifier string) string {
	return gcpURNPrefix + kind + ":" + identifier
}

// gcpHash returns the hexadecimal SHA-256 of value, as the sha256sum template function does.
func gcpHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// gcpRelationship returns the relationship extra a mapping must render between source and target URNs.
func gcpRelationship(source, relationshipType, target string) mapper.ExtraMappedData {
	return mapper.ExtraMappedData{
		APIVersion: RelationshipsAPIVersion,
		ItemFamily: RelationshipsItemFamily,
		Identifier: gcpHash(source + "-" + relationshipType + "-" + target),
		Spec:       map[string]any{"sourceRef": source, "targetRef": target, "typeRef": relationshipType},
	}
}

// gcpRenderCase is one fixture and the item the bundled mapping must render out of it.
type gcpRenderCase struct {
	fixture            string
	expectedIdentifier string
	expectedTitle      string
	expectedSpec       map[string]any
	expectedExtras     []mapper.ExtraMappedData
}

// assertGCPMapping checks type, item type and the rendering of every case through the real mapper.
func assertGCPMapping(t *testing.T, name, expectedType, expectedFamily string, testCases map[string]gcpRenderCase) {
	t.Helper()

	m, mapping := loadGCPMapper(t, name)
	require.Equal(t, expectedType, mapping.Type)
	require.Equal(t, "gcp.mia-platform.eu/v1", mapping.APIVersion)
	require.Equal(t, expectedFamily, mapping.ItemFamily)

	for caseName, testCase := range testCases {
		t.Run(caseName, func(t *testing.T) {
			t.Parallel()

			asset := loadGCPFixture(t, testCase.fixture)
			output, extras, err := m.ApplyTemplates(asset, mapper.ParentItemInfo{})
			require.NoError(t, err)
			require.Equal(t, testCase.expectedIdentifier, output.Identifier)
			require.Equal(t, map[string]any{"title": testCase.expectedTitle}, output.Metadata)
			require.Equal(t, testCase.expectedSpec, output.Spec)
			require.ElementsMatch(t, testCase.expectedExtras, extras)

			// a delete renders the identifiers from the same payload and, as every relationship is
			// deleted in cascade, deletes all the relationships the mapping declares, existing or not
			deleteIdentifier, deleteExtras, err := m.ApplyIdentifierTemplate(asset)
			require.NoError(t, err)
			require.Equal(t, testCase.expectedIdentifier, deleteIdentifier)
			require.Len(t, deleteExtras, len(mapping.Mappings.Extra))
			for _, expected := range testCase.expectedExtras {
				require.Contains(t, deleteExtras, mapper.ExtraMappedData{
					APIVersion: expected.APIVersion,
					ItemFamily: expected.ItemFamily,
					Identifier: expected.Identifier,
				})
			}
		})
	}
}

func TestGCPDNSZoneMapping(t *testing.T) {
	t.Parallel()

	assertGCPMapping(t, "dnszones", "dns.googleapis.com/ManagedZone", "dnszones", map[string]gcpRenderCase{
		"public zone": {
			fixture:            "managedzone-public.yaml",
			expectedIdentifier: "123456789012-1111222233334444555",
			expectedTitle:      "example-zone",
			expectedSpec: map[string]any{
				"name":              "example-zone",
				"id":                "1111222233334444555",
				"dnsName":           "example.com.",
				"description":       "The public example DNS",
				"visibility":        "public",
				"nameServers":       []any{"ns-cloud-a1.googledomains.com.", "ns-cloud-a2.googledomains.com."},
				"dnssecState":       "on",
				"privateNetworks":   []any{},
				"forwardingEnabled": false,
				"peeringEnabled":    false,
				"creationTime":      "2024-11-20T13:55:03.069Z",
				"location":          "global",
				"labels":            map[string]any{"area": "network", "environment": "public"},
			},
		},
		"private zone without description, labels and dnssec": {
			fixture:            "managedzone-private.yaml",
			expectedIdentifier: "123456789012-6666777788889999000",
			expectedTitle:      "internal-zone",
			expectedSpec: map[string]any{
				"name":              "internal-zone",
				"id":                "6666777788889999000",
				"dnsName":           "internal.example.com.",
				"description":       "",
				"visibility":        "private",
				"nameServers":       []any{},
				"dnssecState":       "off",
				"privateNetworks":   []any{"projects/example-network-host/global/networks/my-network"},
				"forwardingEnabled": true,
				"peeringEnabled":    false,
				"creationTime":      "2025-01-10T09:00:00.000Z",
				"location":          "global",
				"labels":            map[string]any{},
			},
		},
	})
}

func TestGCPSnapshotMapping(t *testing.T) {
	t.Parallel()

	assertGCPMapping(t, "snapshots", "compute.googleapis.com/Snapshot", "snapshots", map[string]gcpRenderCase{
		"global snapshot created by a schedule": {
			fixture:            "snapshot-global-scheduled.yaml",
			expectedIdentifier: "example-project-my-disk-europe-west1-c-20261014013223-abcd1234",
			expectedExtras:     []mapper.ExtraMappedData{gcpRelationship(gcpURN("Snapshot", "example-project-my-disk-europe-west1-c-20261014013223-abcd1234"), relationshipDepends, gcpURN("Disk", "example-project-europe-west1-c-my-disk"))},
			expectedTitle:      "my-disk-europe-west1-c-20261014013223-abcd1234",
			expectedSpec: map[string]any{
				"name":              "my-disk-europe-west1-c-20261014013223-abcd1234",
				"description":       "",
				"status":            "ready",
				"snapshotType":      "standard",
				"sourceDisk":        "projects/example-project/zones/europe-west1-c/disks/my-disk",
				"diskSizeGb":        50,
				"storageBytes":      572360896,
				"storageLocations":  []any{"eu"},
				"autoCreated":       true,
				"schedulePolicy":    "four-times-a-day",
				"architecture":      "X86_64",
				"licenses":          []any{"debian-12-bookworm"},
				"creationTimestamp": "2026-10-13T18:32:23.856-07:00",
				"location":          "eu",
				"labels":            map[string]any{},
			},
		},
		"regional manual snapshot with only the mandatory fields": {
			fixture:            "snapshot-regional-manual.yaml",
			expectedIdentifier: "example-project-europe-west1-my-manual-snapshot",
			expectedTitle:      "my-manual-snapshot",
			expectedSpec: map[string]any{
				"name":              "my-manual-snapshot",
				"description":       "Taken before the upgrade",
				"status":            "uploading",
				"snapshotType":      "archive",
				"sourceDisk":        "",
				"diskSizeGb":        0,
				"storageBytes":      0,
				"storageLocations":  []any{},
				"autoCreated":       false,
				"schedulePolicy":    "",
				"architecture":      "",
				"licenses":          []any{},
				"creationTimestamp": "2026-10-01T10:00:00.000-07:00",
				"location":          "europe-west1",
				"labels":            map[string]any{"owner": "platform"},
			},
		},
	})
}

func TestGCPAddressMapping(t *testing.T) {
	t.Parallel()

	assertGCPMapping(t, "addresses", "compute.googleapis.com/Address", "addresses", map[string]gcpRenderCase{
		"nat address used by a router": {
			fixture:            "address-nat-auto.yaml",
			expectedIdentifier: "example-project-europe-west1-nat-auto-ip-16480479-2-1679314465674173",
			expectedExtras:     []mapper.ExtraMappedData{gcpRelationship(gcpURN("Router", "example-project-europe-west1-my-router"), relationshipDepends, gcpURN("Address", "example-project-europe-west1-nat-auto-ip-16480479-2-1679314465674173"))},
			expectedTitle:      "nat-auto-ip-16480479-2-1679314465674173",
			expectedSpec: map[string]any{
				"name":              "nat-auto-ip-16480479-2-1679314465674173",
				"description":       "",
				"address":           "203.0.113.10",
				"addressType":       "external",
				"ipVersion":         "ipv4",
				"purpose":           "nat_auto",
				"status":            "in_use",
				"networkTier":       "premium",
				"network":           "",
				"subnetwork":        "",
				"users":             []any{"projects/example-project/regions/europe-west1/routers/my-router"},
				"creationTimestamp": "2023-03-20T05:14:25.762-07:00",
				"location":          "europe-west1",
				"labels":            map[string]any{},
			},
		},
		"internal address used by a virtual machine": {
			fixture:            "address-internal-vm.yaml",
			expectedIdentifier: "example-project-europe-west1-my-internal-ip",
			expectedExtras:     []mapper.ExtraMappedData{gcpRelationship(gcpURN("ComputeInstance", "example-project-my-vm"), relationshipDepends, gcpURN("Address", "example-project-europe-west1-my-internal-ip"))},
			expectedTitle:      "my-internal-ip",
			expectedSpec: map[string]any{
				"name":              "my-internal-ip",
				"description":       "Internal address of my-vm",
				"address":           "10.0.0.5",
				"addressType":       "internal",
				"ipVersion":         "ipv4",
				"purpose":           "gce_endpoint",
				"status":            "in_use",
				"networkTier":       "",
				"network":           "projects/example-project/global/networks/my-network",
				"subnetwork":        "projects/example-project/regions/europe-west1/subnetworks/my-subnet",
				"users":             []any{"projects/example-project/zones/europe-west1-b/instances/my-vm"},
				"creationTimestamp": "2024-05-01T08:00:00.000-07:00",
				"location":          "europe-west1",
				"labels":            map[string]any{"team": "platform"},
			},
		},
		"address used by a virtual machine, a router and a forwarding rule": {
			fixture:            "address-router-and-vm.yaml",
			expectedIdentifier: "example-project-europe-west1-my-shared-ip",
			expectedTitle:      "my-shared-ip",
			expectedSpec: map[string]any{
				"name":    "my-shared-ip",
				"address": "203.0.113.40",
				"users": []any{
					"projects/example-project/zones/europe-west1-b/instances/my-vm",
					"projects/example-project/regions/europe-west1/routers/my-router",
					"projects/example-project/regions/europe-west1/forwardingRules/my-rule",
				},
				"description": "", "addressType": "external", "ipVersion": "ipv4", "purpose": "", "status": "in_use",
				"networkTier": "", "network": "", "subnetwork": "",
				"creationTimestamp": "2024-07-01T08:00:00.000-07:00", "location": "europe-west1", "labels": map[string]any{},
			},
			expectedExtras: []mapper.ExtraMappedData{
				gcpRelationship(gcpURN("Router", "example-project-europe-west1-my-router"), relationshipDepends, gcpURN("Address", "example-project-europe-west1-my-shared-ip")),
				gcpRelationship(gcpURN("ComputeInstance", "example-project-my-vm"), relationshipDepends, gcpURN("Address", "example-project-europe-west1-my-shared-ip")),
			},
		},
		"reserved address with only the mandatory fields": {
			fixture:            "address-reserved.yaml",
			expectedIdentifier: "example-project-europe-west1-my-reserved-ip",
			expectedTitle:      "my-reserved-ip",
			expectedSpec: map[string]any{
				"name":              "my-reserved-ip",
				"description":       "",
				"address":           "203.0.113.20",
				"addressType":       "external",
				"ipVersion":         "ipv4",
				"purpose":           "",
				"status":            "reserved",
				"networkTier":       "",
				"network":           "",
				"subnetwork":        "",
				"users":             []any{},
				"creationTimestamp": "2024-06-01T08:00:00.000-07:00",
				"location":          "europe-west1",
				"labels":            map[string]any{},
			},
		},
	})
}

func TestGCPRouterMapping(t *testing.T) {
	t.Parallel()

	assertGCPMapping(t, "routers", "compute.googleapis.com/Router", "routers", map[string]gcpRenderCase{
		"router with an automatic nat": {
			fixture:            "router-nat-auto.yaml",
			expectedIdentifier: "example-project-europe-west1-my-router",
			expectedExtras:     []mapper.ExtraMappedData{gcpRelationship(gcpURN("Router", "example-project-europe-west1-my-router"), relationshipPartOf, gcpURN("Network", "example-project-my-network"))},
			expectedTitle:      "my-router",
			expectedSpec: map[string]any{
				"name":        "my-router",
				"description": "",
				"network":     "projects/example-project/global/networks/my-network",
				"bgp":         map[string]any{},
				"natCount":    1,
				"nats": []any{
					map[string]any{
						"name":                             "my-nat",
						"type":                             "PUBLIC",
						"natIpAllocateOption":              "AUTO_ONLY",
						"sourceSubnetworkIpRangesToNat":    "ALL_SUBNETWORKS_ALL_IP_RANGES",
						"autoNetworkTier":                  "PREMIUM",
						"minPortsPerVm":                    128,
						"enableEndpointIndependentMapping": false,
						"endpointTypes":                    []any{"ENDPOINT_TYPE_VM"},
						"logConfig":                        map[string]any{"enable": false, "filter": "ALL"},
						"natIps":                           []any{},
						"drainNatIps":                      []any{},
					},
				},
				"creationTimestamp": "2023-03-20T04:35:15.379-07:00",
				"location":          "europe-west1",
			},
		},
		"router with bgp and manual and automatic nats": {
			fixture:            "router-nat-manual.yaml",
			expectedIdentifier: "example-project-europe-west4-my-manual-router",
			expectedExtras:     []mapper.ExtraMappedData{gcpRelationship(gcpURN("Router", "example-project-europe-west4-my-manual-router"), relationshipPartOf, gcpURN("Network", "example-project-my-network"))},
			expectedTitle:      "my-manual-router",
			expectedSpec: map[string]any{
				"name":        "my-manual-router",
				"description": "Router with manual NAT",
				"network":     "projects/example-project/global/networks/my-network",
				"bgp":         map[string]any{"asn": 64514, "advertiseMode": "DEFAULT", "keepaliveInterval": 20},
				"natCount":    2,
				"nats": []any{
					map[string]any{
						"name":                          "my-manual-nat",
						"natIpAllocateOption":           "MANUAL_ONLY",
						"sourceSubnetworkIpRangesToNat": "LIST_OF_SUBNETWORKS",
						"subnetworks": []any{
							map[string]any{
								"name":                "https://www.googleapis.com/compute/v1/projects/example-project/regions/europe-west4/subnetworks/my-subnet",
								"sourceIpRangesToNat": []any{"ALL_IP_RANGES"},
							},
						},
						"minPortsPerVm":               64,
						"maxPortsPerVm":               4096,
						"enableDynamicPortAllocation": true,
						"natIps": []any{
							"projects/example-project/regions/europe-west4/addresses/my-nat-ip-1",
							"projects/example-project/regions/europe-west4/addresses/my-nat-ip-2",
						},
						"drainNatIps": []any{"projects/example-project/regions/europe-west4/addresses/my-old-nat-ip"},
					},
					map[string]any{
						"name":                "my-second-nat",
						"natIpAllocateOption": "AUTO_ONLY",
						"natIps":              []any{},
						"drainNatIps":         []any{},
					},
				},
				"creationTimestamp": "2024-02-01T04:00:00.000-07:00",
				"location":          "europe-west4",
			},
		},
		"router without nats, bgp, network and description": {
			fixture:            "router-empty.yaml",
			expectedIdentifier: "example-project-europe-west1-my-empty-router",
			expectedTitle:      "my-empty-router",
			expectedSpec: map[string]any{
				"name":              "my-empty-router",
				"description":       "",
				"network":           "",
				"bgp":               map[string]any{},
				"natCount":          0,
				"nats":              []any{},
				"creationTimestamp": "2024-03-01T04:00:00.000-07:00",
				"location":          "europe-west1",
			},
		},
	})
}

func TestGCPClusterMapping(t *testing.T) {
	t.Parallel()

	assertGCPMapping(t, "clusters", "container.googleapis.com/Cluster", "clusters", map[string]gcpRenderCase{
		"regional cluster": {
			fixture:            "cluster-regional.yaml",
			expectedIdentifier: "example-project-europe-west1-my-cluster",
			expectedTitle:      "my-cluster",
			// the item of the cluster is unchanged by the relationship: this is the output of the mapping
			// as it was before the relationship was added
			expectedSpec: map[string]any{
				"name":             "my-cluster",
				"description":      "The cluster that hosts example resources",
				"autopilot":        false,
				"apiServerVersion": "1.35.8-gke.1439001",
				"network":          "projects/example-project/global/networks/my-network",
				"subnetwork":       "projects/example-project/regions/europe-west1/subnetworks/my-subnet",
				"podsIpv4Cidr":     "10.254.0.0/18",
				"servicesIpv4Cidr": "10.255.0.0/20",
				"clusterEndpoints": map[string]any{
					"dnsEndpointConfig": map[string]any{"allowExternalTraffic": false, "endpoint": "gke-example.europe-west1.gke.goog"},
					"ipEndpointsConfig": map[string]any{"enablePublicEndpoint": true, "privateEndpoint": "192.168.254.2", "publicEndpoint": "203.0.113.30"},
				},
				"status":   "running",
				"location": "europe-west1",
				"labels":   map[string]any{"billing_area": "example"},
			},
			expectedExtras: []mapper.ExtraMappedData{
				gcpRelationship(gcpURN("Cluster", "example-project-europe-west1-my-cluster"), relationshipDepends, gcpURN("Network", "example-project-my-network")),
			},
		},
	})
}

// TestGCPRelationshipIdentifiersOnSparsePayloads guards the delete path: createIf is not evaluated on a
// delete and an extra identifier that fails to render aborts the delete of the item itself, so the
// relationship identifiers must render, as valid identifiers, even from a payload missing every optional field.
func TestGCPRelationshipIdentifiersOnSparsePayloads(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		mapping string
		name    string
	}{
		"router":     {mapping: "routers", name: "//compute.googleapis.com/projects/example-project/regions/europe-west1/routers/my-router"},
		"address":    {mapping: "addresses", name: "//compute.googleapis.com/projects/example-project/regions/europe-west1/addresses/my-address"},
		"snapshot":   {mapping: "snapshots", name: "//compute.googleapis.com/projects/example-project/global/snapshots/my-snapshot"},
		"disk":       {mapping: "disks", name: "//compute.googleapis.com/projects/example-project/zones/europe-west1-d/disks/my-disk"},
		"cluster":    {mapping: "clusters", name: "//container.googleapis.com/projects/example-project/locations/europe-west1/clusters/my-cluster"},
		"record set": {mapping: "dnsrecordsets", name: "//dns.googleapis.com/projects/123456789012/locations/global/managedZones/1111222233334444555/rrsets/git.example.com./A"},
		"record set with a name too short to hold a zone": {mapping: "dnsrecordsets", name: "//dns.googleapis.com/projects/123456789012"},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			m, mapping := loadGCPMapper(t, testCase.mapping)
			sparse := map[string]any{
				"name":     testCase.name,
				"resource": map[string]any{"data": map[string]any{}},
			}

			identifier, extras, err := m.ApplyIdentifierTemplate(sparse)
			require.NoError(t, err)
			require.NotEmpty(t, identifier)
			require.Len(t, extras, len(mapping.Mappings.Extra))
			for _, extra := range extras {
				require.Equal(t, RelationshipsAPIVersion, extra.APIVersion)
				require.Equal(t, RelationshipsItemFamily, extra.ItemFamily)
				require.Regexp(t, "^[0-9a-f]{64}$", extra.Identifier)
			}
		})
	}
}

const dnsZoneIdentifier = "123456789012-1111222233334444555"

func TestGCPDNSRecordSetMapping(t *testing.T) {
	t.Parallel()

	recordSetURN := func(assetName string) string { return gcpURN("DnsRecordSet", gcpHash(assetName)) }
	const assetPrefix = "//dns.googleapis.com/projects/123456789012/locations/global/managedZones/1111222233334444555/rrsets/"

	assertGCPMapping(t, "dnsrecordsets", "dns.googleapis.com/ResourceRecordSet", "dnsrecordsets", map[string]gcpRenderCase{
		"a record": {
			fixture:            "recordset-a.yaml",
			expectedIdentifier: gcpHash(assetPrefix + "git.example.com./A"),
			expectedTitle:      "git.example.com",
			expectedSpec: map[string]any{
				"name":          "git.example.com.",
				"type":          "A",
				"ttl":           3600,
				"rrdatas":       []any{"203.0.113.50"},
				"routingPolicy": map[string]any{},
				"zoneId":        "1111222233334444555",
				"location":      "global",
			},
			expectedExtras: []mapper.ExtraMappedData{
				gcpRelationship(recordSetURN(assetPrefix+"git.example.com./A"), relationshipPartOf, gcpURN("DnsZone", dnsZoneIdentifier)),
			},
		},
		"wildcard txt record whose name and data are not plain yaml": {
			fixture:            "recordset-wildcard-txt.yaml",
			expectedIdentifier: gcpHash(assetPrefix + "*.example.com./TXT"),
			expectedTitle:      "*.example.com",
			expectedSpec: map[string]any{
				"name":          "*.example.com.",
				"type":          "TXT",
				"ttl":           300,
				"rrdatas":       []any{`"v=spf1 include:_spf.example.com ~all"`, `"second: value"`},
				"routingPolicy": map[string]any{},
				"zoneId":        "1111222233334444555",
				"location":      "global",
			},
			expectedExtras: []mapper.ExtraMappedData{
				gcpRelationship(recordSetURN(assetPrefix+"*.example.com./TXT"), relationshipPartOf, gcpURN("DnsZone", dnsZoneIdentifier)),
			},
		},
		"record set with a routing policy and no data": {
			fixture:            "recordset-routing.yaml",
			expectedIdentifier: gcpHash(assetPrefix + "lb.example.com./A"),
			expectedTitle:      "lb.example.com",
			expectedSpec: map[string]any{
				"name":    "lb.example.com.",
				"type":    "A",
				"ttl":     60,
				"rrdatas": []any{},
				"routingPolicy": map[string]any{
					"wrr": map[string]any{
						"items": []any{
							map[string]any{"rrdatas": []any{"203.0.113.60"}, "weight": 1},
							map[string]any{"rrdatas": []any{"203.0.113.61"}, "weight": 2},
						},
					},
				},
				"zoneId":   "1111222233334444555",
				"location": "global",
			},
			expectedExtras: []mapper.ExtraMappedData{
				gcpRelationship(recordSetURN(assetPrefix+"lb.example.com./A"), relationshipPartOf, gcpURN("DnsZone", dnsZoneIdentifier)),
			},
		},
	})
}

// TestGCPDNSRecordSetRelatesToTheZoneIdentifier checks the join between the two DNS mappings: the zone item is
// written under the identifier the relationship of its record sets points at, although a record set only knows
// the project number and the numeric id of its zone.
func TestGCPDNSRecordSetRelatesToTheZoneIdentifier(t *testing.T) {
	t.Parallel()

	zoneMapper, _ := loadGCPMapper(t, "dnszones")
	zoneOutput, _, err := zoneMapper.ApplyTemplates(loadGCPFixture(t, "managedzone-public.yaml"), mapper.ParentItemInfo{})
	require.NoError(t, err)
	require.Equal(t, dnsZoneIdentifier, zoneOutput.Identifier)

	recordSetMapper, _ := loadGCPMapper(t, "dnsrecordsets")
	_, extras, err := recordSetMapper.ApplyTemplates(loadGCPFixture(t, "recordset-a.yaml"), mapper.ParentItemInfo{})
	require.NoError(t, err)
	require.Len(t, extras, 1)
	require.Equal(t, gcpURN("DnsZone", zoneOutput.Identifier), extras[0].Spec["targetRef"])
}

func TestGCPDiskMapping(t *testing.T) {
	t.Parallel()

	assertGCPMapping(t, "disks", "compute.googleapis.com/Disk", "disks", map[string]gcpRenderCase{
		"zonal disk attached to a virtual machine": {
			fixture:            "disk-attached.yaml",
			expectedIdentifier: "example-project-europe-west1-d-my-vm",
			expectedTitle:      "my-vm",
			expectedExtras:     []mapper.ExtraMappedData{gcpRelationship(gcpURN("Disk", "example-project-europe-west1-d-my-vm"), relationshipDepends, gcpURN("ComputeInstance", "example-project-my-vm"))},
			expectedSpec: map[string]any{
				"name":                  "my-vm",
				"description":           "",
				"status":                "ready",
				"diskType":              "pd-standard",
				"sizeGb":                50,
				"architecture":          "X86_64",
				"sourceImage":           "debian-12-bookworm-v20261010",
				"sourceSnapshot":        "",
				"provisionedIops":       0,
				"provisionedThroughput": 0,
				"replicaZones":          []any{},
				"users":                 []any{"projects/example-project/zones/europe-west1-d/instances/my-vm"},
				"creationTimestamp":     "2026-10-26T21:12:01.081-07:00",
				"lastAttachTimestamp":   "2026-10-26T21:12:02.090-07:00",
				"lastDetachTimestamp":   "",
				"location":              "europe-west1-d",
				"labels":                map[string]any{"owner": "platform"},
			},
		},
		"regional detached disk": {
			fixture:            "disk-regional-detached.yaml",
			expectedIdentifier: "example-project-europe-west1-my-data-disk",
			expectedTitle:      "my-data-disk",
			expectedSpec: map[string]any{
				"name":                  "my-data-disk",
				"description":           "",
				"status":                "ready",
				"diskType":              "pd-balanced",
				"sizeGb":                200,
				"architecture":          "",
				"sourceImage":           "",
				"sourceSnapshot":        "my-manual-snapshot",
				"provisionedIops":       3000,
				"provisionedThroughput": 140,
				"replicaZones":          []any{"europe-west1-b", "europe-west1-c"},
				"users":                 []any{},
				"creationTimestamp":     "2026-09-01T08:00:00.000-07:00",
				"lastAttachTimestamp":   "",
				"lastDetachTimestamp":   "",
				"location":              "europe-west1",
				"labels":                map[string]any{},
			},
		},
	})
}
