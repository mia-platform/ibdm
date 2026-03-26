// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package gitlab

import "time"

const (
	// gitlabEventHeader is the header name carrying the event type.
	gitlabEventHeader = "X-Gitlab-Event"

	// gitlabTokenHeader is the header name carrying the plain-text secret token.
	//nolint:gosec // this is a header name, not a credential value
	gitlabTokenHeader = "X-Gitlab-Token"
)

// gitlabEvent is implemented by all GitLab webhook event types.
type gitlabEvent interface {
	EventTime() time.Time
}
