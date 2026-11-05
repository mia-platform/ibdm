// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

// Package jenkins implements a Mia-Platform IBDM source for a self-hosted Jenkins controller.
// It supports pull-based synchronisation through the Jenkins JSON REST API, authenticating with
// a username and an API token.
//
// The source walks the folder tree of the controller and emits the following data types:
//
//   - instance: the controller itself, with its version read from the X-Jenkins response header.
//   - folder: folders and organization folders, with a reference to their parent.
//   - multibranch: multibranch projects, with their primary branch and the number of branches,
//     pull requests and tags.
//   - workflow: standalone pipeline jobs and the primary branch job of every multibranch
//     project, with their last completed build and the stages of that build.
//   - sharedlibrary: the shared libraries loaded by the last completed builds, with the
//     workflows that load them.
//   - credential: the metadata of the credentials stored at system and folder level, with the
//     workflows and multibranch projects that use them. Secret values are never requested.
//   - plugin: the installed plugins with their version.
//
// Every item that has a parent carries a parent key shaped as {kind, fullName, url}, where kind
// is either instance or folder. The primary branch workflow of a multibranch project carries a
// multibranch key instead.
package jenkins
