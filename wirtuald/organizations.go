package wirtuald

import (
	"net/http"

	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/db2sdk"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/httpapi"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/httpmw"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

// @Summary Get organizations
// @ID get-organizations
// @Security CoderSessionToken
// @Produce json
// @Tags Organizations
// @Success 200 {object} []wirtualsdk.Organization
// @Router /organizations [get]
func (api *API) organizations(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	organizations, err := api.Database.GetOrganizations(ctx, database.GetOrganizationsParams{})
	if httpapi.Is404Error(err) {
		httpapi.ResourceNotFound(rw)
		return
	}
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, wirtualsdk.Response{
			Message: "Internal error fetching organizations.",
			Detail:  err.Error(),
		})
		return
	}

	httpapi.Write(ctx, rw, http.StatusOK, db2sdk.List(organizations, db2sdk.Organization))
}

// @Summary Get organization by ID
// @ID get-organization-by-id
// @Security CoderSessionToken
// @Produce json
// @Tags Organizations
// @Param organization path string true "Organization ID" format(uuid)
// @Success 200 {object} wirtualsdk.Organization
// @Router /organizations/{organization} [get]
func (*API) organization(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	organization := httpmw.OrganizationParam(r)

	httpapi.Write(ctx, rw, http.StatusOK, db2sdk.Organization(organization))
}
