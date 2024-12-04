package coderd

import (
	"net/http"

	"github.com/coder/coder/v2/wirtuald/httpapi"
	"github.com/coder/coder/v2/wirtuald/rbac"
	"github.com/coder/coder/v2/wirtuald/rbac/policy"
	"github.com/coder/coder/v2/wirtualsdk"
)

// @Summary Get deployment config
// @ID get-deployment-config
// @Security CoderSessionToken
// @Produce json
// @Tags General
// @Success 200 {object} wirtualsdk.DeploymentConfig
// @Router /deployment/config [get]
func (api *API) deploymentValues(rw http.ResponseWriter, r *http.Request) {
	if !api.Authorize(r, policy.ActionRead, rbac.ResourceDeploymentConfig) {
		httpapi.Forbidden(rw)
		return
	}

	values, err := api.DeploymentValues.WithoutSecrets()
	if err != nil {
		httpapi.InternalServerError(rw, err)
		return
	}

	httpapi.Write(
		r.Context(), rw, http.StatusOK,
		wirtualsdk.DeploymentConfig{
			Values:  values,
			Options: api.DeploymentOptions,
		},
	)
}

// @Summary Get deployment stats
// @ID get-deployment-stats
// @Security CoderSessionToken
// @Produce json
// @Tags General
// @Success 200 {object} wirtualsdk.DeploymentStats
// @Router /deployment/stats [get]
func (api *API) deploymentStats(rw http.ResponseWriter, r *http.Request) {
	if !api.Authorize(r, policy.ActionRead, rbac.ResourceDeploymentStats) {
		httpapi.Forbidden(rw)
		return
	}

	stats, ok := api.metricsCache.DeploymentStats()
	if !ok {
		httpapi.Write(r.Context(), rw, http.StatusBadRequest, wirtualsdk.Response{
			Message: "Deployment stats are still processing!",
		})
		return
	}

	httpapi.Write(r.Context(), rw, http.StatusOK, stats)
}

// @Summary Build info
// @ID build-info
// @Produce json
// @Tags General
// @Success 200 {object} wirtualsdk.BuildInfoResponse
// @Router /buildinfo [get]
func buildInfoHandler(resp wirtualsdk.BuildInfoResponse) http.HandlerFunc {
	// This is in a handler so that we can generate API docs info.
	return func(rw http.ResponseWriter, r *http.Request) {
		httpapi.Write(r.Context(), rw, http.StatusOK, resp)
	}
}

// @Summary SSH Config
// @ID ssh-config
// @Security CoderSessionToken
// @Produce json
// @Tags General
// @Success 200 {object} wirtualsdk.SSHConfigResponse
// @Router /deployment/ssh [get]
func (api *API) sshConfig(rw http.ResponseWriter, r *http.Request) {
	httpapi.Write(r.Context(), rw, http.StatusOK, api.SSHConfig)
}
