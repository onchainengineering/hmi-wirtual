package httpmw

import (
	"fmt"
	"net/http"

	"github.com/coder/coder/v2/coderd/httpapi"
	"github.com/coder/coder/v2/wirtualsdk"
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
