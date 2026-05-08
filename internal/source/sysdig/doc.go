// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

// Package sysdig provides a source implementation to integrate with Sysdig Secure.
// It queries the SysQL API to fetch vulnerability data for container images and
// pushes results through IBDM's standard pipeline. It also accepts webhook
// notifications from Sysdig pipeline scans, fetching full vulnerability results
// from the Sysdig Vulnerability API.
package sysdig
