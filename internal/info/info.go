// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

// Package info holds application version information.
package info

var (
	// AppName is the name of the application.
	AppName = "ibdm"
	// Version is dynamically set by the ci or overridden by the Makefile.
	Version = "DEV"
	// BuildDate is dynamically set at build time by the cli or overridden in the Makefile.
	BuildDate = "" // YYYY-MM-DD
)
