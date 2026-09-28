// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

// Package mappings holds the mapping configurations bundled with ibdm. The files are embedded in
// the binary, one directory per source, and are parsed with the same loader used for the mapping
// files passed on the command line. A source is a system source if and only if ibdm bundles
// mappings for it.
package mappings
