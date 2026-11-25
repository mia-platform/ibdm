// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

// Package server contains the HTTP server of the IBDM application, built on net/http.
// It serves the webhooks registered by the sources and the health and readiness routes, through
// a chain of standard http.Handler middleware that logs every request.
// The server is made of listeners, each with its own address, routes and middleware chain;
// today a single listener serves everything.
package server
