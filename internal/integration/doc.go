// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

// Package integration holds the command-level integration tests of ibdm.
//
// The tests build the real ibdm binary once, with the race detector, and run it as a subprocess
// with real flags and an explicit environment, against fake upstream systems and a fake
// Mia-Platform Catalog served by httptest. They assert on the traffic the fakes record and on the
// logs the binary writes, and compare the normalised traffic with golden files under testdata.
//
// Every test file carries the integration build tag, so the tests run only when asked for:
//
//	make test-integration
//	go test -tags=integration -race -count=1 ./internal/integration/...
//
// Golden files are rewritten with the update flag, and must be reviewed before they are kept:
//
//	go test -tags=integration -race -count=1 ./internal/integration/... -update
//
// The binary never inherits the environment of the developer running the tests, so no real
// credential can reach it. All test data is fictional.
package integration
