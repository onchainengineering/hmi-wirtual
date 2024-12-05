package wirtuald

import (
	"net/http"

	"github.com/onchainengineering/hmi-wirtual/wirtuald/httpapi"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

// @Summary API root handler
// @ID api-root-handler
// @Produce json
// @Tags General
// @Success 200 {object} wirtualsdk.Response
// @Router / [get]
func apiRoot(w http.ResponseWriter, r *http.Request) {
	httpapi.Write(r.Context(), w, http.StatusOK, wirtualsdk.Response{
		//nolint:gocritic
		Message: "👋",
	})
}
