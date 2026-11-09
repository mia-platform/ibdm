// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package jenkins

import (
	"net/http"
	"strings"
)

// The fixtures describe a fictional controller at https://ci.example.com:
//
//	my-team/                      folder
//	  my-service                  workflow, last completed build #12 failed in the Test stage
//	  my-app/                     multibranch, primary branch main (#40), branch feature/login, PR-7
//	  legacy-job                  freestyle job, skipped
//	my-org/                       organization folder
//	  my-repo/                    multibranch without primary branch
//	root-pipeline                 workflow without completed builds
const (
	rootListingPath      = "/api/json"
	myTeamListingPath    = "/job/my-team/api/json"
	myAppListingPath     = "/job/my-team/job/my-app/api/json"
	myAppMainPath        = "/job/my-team/job/my-app/job/main/api/json"
	myOrgListingPath     = "/job/my-org/api/json"
	myRepoListingPath    = "/job/my-org/job/my-repo/api/json"
	myServiceRunPath     = "/job/my-team/job/my-service/12/wfapi/describe"
	myServiceTestPath    = "/job/my-team/job/my-service/12/execution/node/15/wfapi/describe"
	myAppMainRunPath     = "/job/my-team/job/my-app/job/main/40/wfapi/describe"
	systemStoreAPIPath   = "/credentials/store/system/api/json"
	myTeamStoreAPIPath   = "/job/my-team/credentials/store/folder/api/json"
	pluginManagerAPIPath = "/pluginManager/api/json"
)

const instanceFixture = `{
  "_class": "hudson.model.Hudson",
  "description": "Welcome to the my-org build controller.",
  "mode": "NORMAL"
}`

const rootListingFixture = `{
  "_class": "hudson.model.Hudson",
  "jobs": [
    {
      "_class": "com.cloudbees.hudson.plugins.folder.Folder",
      "name": "my-team",
      "fullName": "my-team",
      "displayName": "My Team",
      "description": "Pipelines of my team",
      "url": "https://ci.example.com/job/my-team/",
      "healthReport": [],
      "actions": [{}, {"_class": "com.cloudbees.plugins.credentials.ViewCredentialsAction"}],
      "jobs": [{"name": "my-service"}, {"name": "my-app"}, {"name": "legacy-job"}]
    },
    {
      "_class": "jenkins.branch.OrganizationFolder",
      "name": "my-org",
      "fullName": "my-org",
      "displayName": "my-org",
      "description": null,
      "url": "https://ci.example.com/job/my-org/",
      "actions": [{"_class": "jenkins.scm.api.metadata.ObjectMetadataAction", "objectUrl": "https://git.example.com/my-org"}],
      "jobs": [{"name": "my-repo"}]
    },
    {
      "_class": "org.jenkinsci.plugins.workflow.job.WorkflowJob",
      "name": "root-pipeline",
      "fullName": "root-pipeline",
      "displayName": "root-pipeline",
      "description": "",
      "url": "https://ci.example.com/job/root-pipeline/",
      "buildable": true,
      "actions": [{}],
      "lastCompletedBuild": null
    }
  ]
}`

const myTeamListingFixture = `{
  "_class": "com.cloudbees.hudson.plugins.folder.Folder",
  "jobs": [
    {
      "_class": "org.jenkinsci.plugins.workflow.job.WorkflowJob",
      "name": "my-service",
      "fullName": "my-team/my-service",
      "displayName": "my-service",
      "description": "Service pipeline",
      "url": "https://ci.example.com/job/my-team/job/my-service/",
      "buildable": true,
      "actions": [{}],
      "lastCompletedBuild": {
        "_class": "org.jenkinsci.plugins.workflow.job.WorkflowRun",
        "number": 12,
        "result": "FAILURE",
        "timestamp": 1767261600000,
        "duration": 95000,
        "url": "https://ci.example.com/job/my-team/job/my-service/12/",
        "actions": [
          {"_class": "hudson.model.CauseAction"},
          {"_class": "org.jenkinsci.plugins.workflow.libs.LibrariesAction", "libraries": [
            {"name": "my-lib", "version": "main", "trusted": true, "changelog": true, "variables": ["deployApp", "buildApp"]}
          ]}
        ]
      }
    },
    {
      "_class": "org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject",
      "name": "my-app",
      "fullName": "my-team/my-app",
      "displayName": "my-app",
      "description": "Application repository",
      "url": "https://ci.example.com/job/my-team/job/my-app/",
      "healthReport": [{"score": 80, "description": "Build stability: 1 out of the last 5 builds failed."}],
      "actions": [{"_class": "jenkins.scm.api.metadata.ObjectMetadataAction", "objectUrl": "https://git.example.com/my-org/my-app"}],
      "jobs": [{"name": "main"}, {"name": "feature%2Flogin"}, {"name": "PR-7"}]
    },
    {
      "_class": "hudson.model.FreeStyleProject",
      "name": "legacy-job",
      "fullName": "my-team/legacy-job",
      "displayName": "legacy-job",
      "url": "https://ci.example.com/job/my-team/job/legacy-job/",
      "buildable": true
    }
  ]
}`

const myAppListingFixture = `{
  "_class": "org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject",
  "jobs": [
    {
      "_class": "org.jenkinsci.plugins.workflow.job.WorkflowJob",
      "name": "main",
      "fullName": "my-team/my-app/main",
      "url": "https://ci.example.com/job/my-team/job/my-app/job/main/",
      "actions": [{"_class": "jenkins.scm.api.metadata.PrimaryInstanceMetadataAction"}, {"_class": "jenkins.scm.api.metadata.ObjectMetadataAction"}]
    },
    {
      "_class": "org.jenkinsci.plugins.workflow.job.WorkflowJob",
      "name": "feature%2Flogin",
      "fullName": "my-team/my-app/feature%2Flogin",
      "url": "https://ci.example.com/job/my-team/job/my-app/job/feature%252Flogin/",
      "actions": [{"_class": "jenkins.scm.api.metadata.ObjectMetadataAction"}]
    },
    {
      "_class": "org.jenkinsci.plugins.workflow.job.WorkflowJob",
      "name": "PR-7",
      "fullName": "my-team/my-app/PR-7",
      "url": "https://ci.example.com/job/my-team/job/my-app/job/PR-7/",
      "actions": [{"_class": "jenkins.scm.api.metadata.ContributorMetadataAction"}]
    }
  ],
  "views": [
    {"name": "default", "jobs": [{"name": "main"}, {"name": "feature%2Flogin"}]},
    {"name": "change-requests", "jobs": [{"name": "PR-7"}]},
    {"name": "tags", "jobs": []}
  ]
}`

const myAppMainFixture = `{
  "_class": "org.jenkinsci.plugins.workflow.job.WorkflowJob",
  "name": "main",
  "fullName": "my-team/my-app/main",
  "displayName": "main",
  "description": null,
  "url": "https://ci.example.com/job/my-team/job/my-app/job/main/",
  "buildable": true,
  "actions": [{"_class": "jenkins.scm.api.metadata.PrimaryInstanceMetadataAction"}],
  "lastCompletedBuild": {
    "number": 40,
    "result": "SUCCESS",
    "timestamp": 1767265200000,
    "duration": 42000,
    "url": "https://ci.example.com/job/my-team/job/my-app/job/main/40/",
    "actions": [
      {"_class": "org.jenkinsci.plugins.workflow.libs.LibrariesAction", "libraries": [
        {"name": "my-lib", "version": "v2", "trusted": true, "changelog": true, "variables": ["deployApp"]},
        {"name": "other-lib", "version": "1.0", "trusted": false, "changelog": false, "variables": []}
      ]}
    ]
  }
}`

const myOrgListingFixture = `{
  "_class": "jenkins.branch.OrganizationFolder",
  "jobs": [
    {
      "_class": "org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject",
      "name": "my-repo",
      "fullName": "my-org/my-repo",
      "displayName": "my-repo",
      "description": "",
      "url": "https://ci.example.com/job/my-org/job/my-repo/",
      "healthReport": [],
      "actions": [],
      "jobs": [{"name": "develop"}]
    }
  ]
}`

const myRepoListingFixture = `{
  "_class": "org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject",
  "jobs": [
    {
      "_class": "org.jenkinsci.plugins.workflow.job.WorkflowJob",
      "name": "develop",
      "fullName": "my-org/my-repo/develop",
      "url": "https://ci.example.com/job/my-org/job/my-repo/job/develop/",
      "actions": []
    }
  ],
  "views": []
}`

const myServiceRunFixture = `{
  "id": "12",
  "name": "#12",
  "status": "FAILED",
  "stages": [
    {"id": "6", "name": "Build", "execNode": "agent-1", "status": "SUCCESS", "startTimeMillis": 1767261601000, "durationMillis": 30000},
    {"id": "15", "name": "Test", "execNode": "agent-1", "status": "FAILED", "startTimeMillis": 1767261631000, "durationMillis": 60000,
     "error": {"message": "script returned exit code 1", "type": "hudson.AbortException"}},
    {"id": "25", "name": "Deploy", "status": "NOT_EXECUTED", "startTimeMillis": 1767261691000, "durationMillis": 0}
  ]
}`

const myServiceTestFixture = `{
  "id": "15",
  "name": "Test",
  "status": "FAILED",
  "stageFlowNodes": [
    {"id": "16", "name": "Print Message", "status": "SUCCESS"},
    {"id": "17", "name": "Shell Script", "status": "FAILED",
     "error": {"message": "script returned exit code 1", "type": "hudson.AbortException"}}
  ]
}`

const myAppMainRunFixture = `{
  "id": "40",
  "name": "#40",
  "status": "SUCCESS",
  "stages": [
    {"id": "5", "name": "Build", "status": "SUCCESS", "startTimeMillis": 1767265201000, "durationMillis": 41000}
  ]
}`

const systemStoreFixture = `{
  "_class": "com.cloudbees.plugins.credentials.CredentialsStoreAction",
  "domains": {
    "_": {
      "_class": "com.cloudbees.plugins.credentials.CredentialsStoreAction$DomainWrapper",
      "credentials": [
        {
          "id": "deploy-key",
          "displayName": "deploy (Deploy key)",
          "typeName": "SSH Username with private key",
          "description": "Deploy key",
          "fingerprint": {
            "usage": [
              {"name": "my-team/my-service"},
              {"name": "my-team/my-app/main"},
              {"name": "my-team/my-app/PR-7"},
              {"name": "my-team/legacy-job"},
              {"name": "deleted-job"}
            ]
          }
        }
      ]
    }
  }
}`

const myTeamStoreFixture = `{
  "_class": "com.cloudbees.plugins.credentials.CredentialsStoreAction",
  "domains": {
    "_": {
      "urlName": "_",
      "credentials": [
        {"id": "team-token", "displayName": "Team token", "typeName": "Secret text", "description": "", "fingerprint": null}
      ]
    }
  }
}`

const pluginsFixture = `{
  "_class": "hudson.LocalPluginManager",
  "plugins": [
    {
      "shortName": "credentials",
      "longName": "Credentials Plugin",
      "version": "1415.v831096eb_5534",
      "active": true,
      "enabled": true,
      "hasUpdate": false,
      "url": "https://plugins.jenkins.io/credentials",
      "requiredCoreVersion": "2.555.1",
      "dependencies": [{"shortName": "structs", "version": "350.v3b_30f09f2363", "optional": false}]
    },
    {
      "shortName": "pipeline-stage-view",
      "longName": "Pipeline: Stage View Plugin",
      "version": "2.38",
      "active": true,
      "enabled": true,
      "hasUpdate": true,
      "url": "https://plugins.jenkins.io/pipeline-stage-view",
      "requiredCoreVersion": "2.479.3",
      "dependencies": []
    }
  ]
}`

// Pipeline Graph View responses for build #12 of my-team/my-service: the Test stage runs Unit and
// Integration in parallel, and Integration runs Setup then Run in sequence; Run fails in step 24.
const (
	myServiceGraphTreePath      = "/job/my-team/job/my-service/12/stages/tree"
	myServiceGraphStepsPath     = "/job/my-team/job/my-service/12/stages/allSteps"
	myServiceGraphExceptionPath = "/job/my-team/job/my-service/12/stages/exceptionText"
	myServiceStepDescribePath   = "/job/my-team/job/my-service/12/execution/node/24/wfapi/describe"
	myAppMainGraphTreePath      = "/job/my-team/job/my-app/job/main/40/stages/tree"
)

const myServiceGraphTreeFixture = `{
  "status": "ok",
  "data": {
    "complete": true,
    "stages": [
      {"id": "6", "name": "Build", "state": "success", "type": "STAGE", "title": "",
       "pauseDurationMillis": 0, "startTimeMillis": 1767261601000, "totalDurationMillis": 30000,
       "children": [], "isSequential": false, "synthetic": false, "placeholder": false,
       "agent": "agent-1", "url": "/job/my-team/job/my-service/12/stages/?selected-node=6"},
      {"id": "15", "name": "Test", "state": "failure", "type": "STAGE", "title": "",
       "pauseDurationMillis": 0, "startTimeMillis": 1767261631000, "totalDurationMillis": 60000,
       "isSequential": false, "synthetic": false, "placeholder": false,
       "url": "/job/my-team/job/my-service/12/stages/?selected-node=15",
       "children": [
         {"id": "18", "name": "Unit", "state": "success", "type": "PARALLEL", "title": "",
          "pauseDurationMillis": 0, "startTimeMillis": 1767261632000, "totalDurationMillis": 20000,
          "children": [], "isSequential": false, "synthetic": false, "placeholder": false,
          "agent": "agent-2", "url": "/job/my-team/job/my-service/12/stages/?selected-node=18"},
         {"id": "19", "name": "Integration", "state": "failure", "type": "PARALLEL", "title": "",
          "pauseDurationMillis": 0, "startTimeMillis": 1767261632000, "totalDurationMillis": 58000,
          "isSequential": false, "synthetic": false, "placeholder": false,
          "agent": "agent-3", "url": "/job/my-team/job/my-service/12/stages/?selected-node=19",
          "children": [
            {"id": "22", "name": "Setup", "state": "success", "type": "STAGE", "title": "",
             "pauseDurationMillis": 0, "startTimeMillis": 1767261633000, "totalDurationMillis": 1000,
             "children": [], "seqContainerName": "Integration", "isSequential": true,
             "synthetic": false, "placeholder": false, "agent": "agent-3",
             "url": "/job/my-team/job/my-service/12/stages/?selected-node=22",
             "nextSibling": {"id": "23", "name": "Run", "state": "failure", "type": "STAGE", "title": "",
               "pauseDurationMillis": 0, "startTimeMillis": 1767261634000, "totalDurationMillis": 56000,
               "children": [], "seqContainerName": "Integration", "isSequential": true,
               "synthetic": false, "placeholder": false, "agent": "agent-3",
               "url": "/job/my-team/job/my-service/12/stages/?selected-node=23"}},
            {"id": "23", "name": "Run", "state": "failure", "type": "STAGE", "title": "",
             "pauseDurationMillis": 0, "startTimeMillis": 1767261634000, "totalDurationMillis": 56000,
             "children": [], "seqContainerName": "Integration", "isSequential": true,
             "synthetic": false, "placeholder": false, "agent": "agent-3",
             "url": "/job/my-team/job/my-service/12/stages/?selected-node=23"}
          ]}
       ]},
      {"id": "25", "name": "Deploy", "state": "skipped", "type": "STAGE", "title": "",
       "pauseDurationMillis": 0, "startTimeMillis": 1767261691000,
       "children": [], "isSequential": false, "synthetic": false, "placeholder": false,
       "url": "/job/my-team/job/my-service/12/stages/?selected-node=25"},
      {"id": "30", "name": "Post Actions", "state": "success", "type": "STAGE", "title": "",
       "pauseDurationMillis": 0, "startTimeMillis": 1767261692000, "totalDurationMillis": 2000,
       "children": [], "isSequential": false, "synthetic": true, "placeholder": false,
       "url": "/job/my-team/job/my-service/12/stages/?selected-node=30"}
    ]
  }
}`

const myServiceGraphStepsFixture = `{
  "status": "ok",
  "data": {
    "runIsComplete": true,
    "steps": [
      {"id": "7", "name": "make build", "state": "success", "type": "STEP", "title": "",
       "pauseDurationMillis": 0, "startTimeMillis": 1767261602000, "totalDurationMillis": 28000, "stageId": "6", "flags": {}},
      {"id": "20", "name": "make unit", "state": "success", "type": "STEP", "title": "",
       "pauseDurationMillis": 0, "startTimeMillis": 1767261633000, "totalDurationMillis": 19000, "stageId": "18", "flags": {}},
      {"id": "24", "name": "make integration", "state": "failure", "type": "STEP", "title": "",
       "pauseDurationMillis": 0, "startTimeMillis": 1767261635000, "totalDurationMillis": 55000, "stageId": "23", "flags": {}}
    ]
  }
}`

const myServiceExceptionText = "hudson.AbortException: script returned exit code 2\n" +
	"\tat org.jenkinsci.plugins.workflow.steps.durable_task.DurableTaskStep$Execution.handleExit(DurableTaskStep.java:668)\n"

const myServiceStepDescribeFixture = `{
  "id": "24", "name": "Shell Script", "status": "FAILED",
  "error": {"message": "script returned exit code 2", "type": "hudson.AbortException"}
}`

const myAppMainGraphTreeFixture = `{
  "status": "ok",
  "data": {
    "complete": true,
    "stages": [
      {"id": "5", "name": "Build", "state": "success", "type": "STAGE", "title": "",
       "pauseDurationMillis": 0, "startTimeMillis": 1767265201000, "totalDurationMillis": 41000,
       "children": [], "isSequential": false, "synthetic": false, "placeholder": false,
       "agent": "agent-1", "url": "/job/my-team/job/my-app/job/main/40/stages/?selected-node=5"}
    ]
  }
}`

// pluginStateFixture returns a plugin list holding the given active and enabled plugins.
func pluginStateFixture(shortNames ...string) string {
	plugins := make([]string, 0, len(shortNames))
	for _, name := range shortNames {
		plugins = append(plugins, `{"shortName": "`+name+`", "active": true, "enabled": true}`)
	}
	return `{"plugins": [` + strings.Join(plugins, ", ") + `]}`
}

// graphViewRoutes returns the fixture routes extended with the Pipeline Graph View responses and
// a plugin list holding the given plugins.
func graphViewRoutes(installed ...string) map[string]fakeResponse {
	return withRoutes(fixtureRoutes(), map[string]fakeResponse{
		pluginManagerAPIPath + "?tree=" + pluginStateTree: {body: pluginStateFixture(installed...)},
		myServiceGraphTreePath:                            {body: myServiceGraphTreeFixture},
		myServiceGraphStepsPath:                           {body: myServiceGraphStepsFixture},
		myServiceGraphExceptionPath:                       {body: myServiceExceptionText},
		myServiceStepDescribePath:                         {body: myServiceStepDescribeFixture},
		myAppMainGraphTreePath:                            {body: myAppMainGraphTreeFixture},
	})
}

// fixtureRoutes returns the routes of the fictional controller.
func fixtureRoutes() map[string]fakeResponse {
	return map[string]fakeResponse{
		rootListingPath + "?tree=" + instanceTree:     {body: instanceFixture, headers: map[string]string{versionHeader: "2.555.1"}},
		rootListingPath + "?tree=" + containerTree:    {body: rootListingFixture},
		myTeamListingPath:                             {body: myTeamListingFixture},
		myAppListingPath + "?tree=" + multibranchTree: {body: myAppListingFixture},
		myAppMainPath:    {body: myAppMainFixture},
		myOrgListingPath: {body: myOrgListingFixture},
		myRepoListingPath + "?tree=" + multibranchTree: {body: myRepoListingFixture},
		myServiceRunPath:     {body: myServiceRunFixture},
		myServiceTestPath:    {body: myServiceTestFixture},
		myAppMainRunPath:     {body: myAppMainRunFixture},
		systemStoreAPIPath:   {body: systemStoreFixture},
		myTeamStoreAPIPath:   {body: myTeamStoreFixture},
		pluginManagerAPIPath: {body: pluginsFixture},
	}
}

// failingResponse is a canned server error.
var failingResponse = fakeResponse{status: http.StatusInternalServerError, body: `{"message":"boom"}`}
