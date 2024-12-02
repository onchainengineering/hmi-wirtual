package wirtuald

import (
	"net/http"

	"github.com/coder/coder/v2/wirtuald/httpapi"
	"github.com/coder/coder/v2/wirtuald/httpmw"
	"github.com/coder/coder/v2/wirtualsdk"
)

// @Summary Removed: Get parameters by template version
// @ID removed-get-parameters-by-template-version
// @Security CoderSessionToken
// @Tags Templates
// @Param templateversion path string true "Template version ID" format(uuid)
// @Success 200
// @Router /templateversions/{templateversion}/parameters [get]
func templateVersionParametersDeprecated(rw http.ResponseWriter, r *http.Request) {
	httpapi.Write(r.Context(), rw, http.StatusOK, []struct{}{})
}

// @Summary Removed: Get schema by template version
// @ID removed-get-schema-by-template-version
// @Security CoderSessionToken
// @Tags Templates
// @Param templateversion path string true "Template version ID" format(uuid)
// @Success 200
// @Router /templateversions/{templateversion}/schema [get]
func templateVersionSchemaDeprecated(rw http.ResponseWriter, r *http.Request) {
	httpapi.Write(r.Context(), rw, http.StatusOK, []struct{}{})
}

// @Summary Removed: Get logs by workspace agent
// @ID removed-get-logs-by-workspace-agent
// @Security CoderSessionToken
// @Produce json
// @Tags Agents
// @Param workspaceagent path string true "Workspace agent ID" format(uuid)
// @Param before query int false "Before log id"
// @Param after query int false "After log id"
// @Param follow query bool false "Follow log stream"
// @Param no_compression query bool false "Disable compression for WebSocket connection"
// @Success 200 {array} wirtualsdk.WorkspaceAgentLog
// @Router /workspaceagents/{workspaceagent}/startup-logs [get]
func (api *API) workspaceAgentLogsDeprecated(rw http.ResponseWriter, r *http.Request) {
	api.workspaceAgentLogs(rw, r)
}

// @Summary Removed: Get workspace agent git auth
// @ID removed-get-workspace-agent-git-auth
// @Security CoderSessionToken
// @Produce json
// @Tags Agents
// @Param match query string true "Match"
// @Param id query string true "Provider ID"
// @Param listen query bool false "Wait for a new token to be issued"
// @Success 200 {object} agentsdk.ExternalAuthResponse
// @Router /workspaceagents/me/gitauth [get]
func (api *API) workspaceAgentsGitAuth(rw http.ResponseWriter, r *http.Request) {
	api.workspaceAgentsExternalAuth(rw, r)
}

// @Summary Removed: Get workspace resources for workspace build
// @ID removed-get-workspace-resources-for-workspace-build
// @Security CoderSessionToken
// @Produce json
// @Tags Builds
// @Param workspacebuild path string true "Workspace build ID"
// @Success 200 {array} wirtualsdk.WorkspaceResource
// @Router /workspacebuilds/{workspacebuild}/resources [get]
// @Deprecated this endpoint is unused and will be removed in future.
func (api *API) workspaceBuildResourcesDeprecated(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceBuild := httpmw.WorkspaceBuildParam(r)

	job, err := api.Database.GetProvisionerJobByID(ctx, workspaceBuild.JobID)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, wirtualsdk.Response{
			Message: "Internal error fetching provisioner job.",
			Detail:  err.Error(),
		})
		return
	}
	api.provisionerJobResources(rw, r, job)
}
