// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

// Package sonarqube provides a source implementation that integrates with
// SonarQube. It accepts the webhook SonarQube sends when an analysis finishes,
// verifies its HMAC signature and reads the issues of the analysed ref back
// from the Web API, since the webhook payload carries none. It can also sync
// the current issues of the main branch of every project.
package sonarqube
