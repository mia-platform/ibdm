// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package console

import (
	"slices"
	"strings"
)

const BaseResourcePath = "console.mia-platform.eu/"

const (
	projectCreatedEvent     = "project_created"
	serviceCreatedEvent     = "service_created"
	tagCreatedEvent         = "tag_created"
	tagDeletedEvent         = "tag_deleted"
	configurationSavedEvent = "configuration_saved"
	companyUserAddedEvent   = "company_user_added"
	companyUserEditedEvent  = "company_user_edited"
	companyUserRemovedEvent = "company_user_removed"

	projectsResource       = BaseResourcePath + "Project"
	servicesResource       = BaseResourcePath + "Service"
	tagsResource           = BaseResourcePath + "Tag"
	configurationsResource = BaseResourcePath + "Configuration"
	companyUsersResource   = BaseResourcePath + "CompanyUser"
)

type event struct {
	EventName string         `json:"eventName"`
	Type      string         `json:"type"`
	Data      map[string]any `json:"data"`
}

func (e event) GetName() string {
	return e.Data["name"].(string)
}

func (e event) GetResource() string {
	switch e.EventName {
	case projectCreatedEvent:
		return projectsResource
	case serviceCreatedEvent:
		return servicesResource
	case tagCreatedEvent:
		return tagsResource
	case tagDeletedEvent:
		return tagsResource
	case configurationSavedEvent:
		return configurationsResource
	case companyUserAddedEvent:
		return companyUsersResource
	case companyUserEditedEvent:
		return companyUsersResource
	case companyUserRemovedEvent:
		return companyUsersResource
	default:
		return e.EventName
	}
}

func (e event) IsTypeIn(types []string) bool {
	eventResource := e.GetResource()
	return slices.ContainsFunc(types, func(s string) bool {
		return strings.EqualFold(s, eventResource)
	})
}
