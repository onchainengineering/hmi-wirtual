package wirtuald

import (
	"context"
	"net/http"

	"github.com/onchainengineering/hmi-wirtual/wirtuald/httpapi"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
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
