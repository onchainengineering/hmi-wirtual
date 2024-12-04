package coderd

import (
	"net/http"

	"github.com/coder/coder/v2/wirtuald/httpapi"
	"github.com/coder/coder/v2/wirtualsdk"
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
