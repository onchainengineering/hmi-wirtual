package httpmw

import (
	"fmt"
	"net/http"

	"github.com/onchainengineering/hmi-wirtual/wirtuald/httpapi"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func RequireExperiment(experiments wirtualsdk.Experiments, experiment wirtualsdk.Experiment) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !experiments.Enabled(experiment) {
				httpapi.Write(r.Context(), w, http.StatusForbidden, wirtualsdk.Response{
					Message: fmt.Sprintf("Experiment '%s' is required but not enabled", experiment),
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
