package coderd

import (
	"context"
	"net/http"

	"github.com/coder/coder/v2/wirtuald/httpapi"
	"github.com/coder/coder/v2/wirtualsdk"
)

func (api *API) shouldBlockNonBrowserConnections(rw http.ResponseWriter) bool {
	if api.Entitlements.Enabled(wirtualsdk.FeatureBrowserOnly) {
		httpapi.Write(context.Background(), rw, http.StatusConflict, wirtualsdk.Response{
			Message: "Non-browser connections are disabled for your deployment.",
		})
		return true
	}
	return false
}
