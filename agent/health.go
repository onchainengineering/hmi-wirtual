package agent

import (
	"net/http"

	"github.com/coder/coder/v2/wirtuald/healthcheck/health"
	"github.com/coder/coder/v2/wirtuald/httpapi"
	"github.com/coder/coder/v2/wirtualsdk"
	"github.com/coder/coder/v2/wirtualsdk/healthsdk"
)

func (a *agent) HandleNetcheck(rw http.ResponseWriter, r *http.Request) {
	ni := a.TailnetConn().GetNetInfo()

	ifReport, err := healthsdk.RunInterfacesReport()
	if err != nil {
		httpapi.Write(r.Context(), rw, http.StatusInternalServerError, wirtualsdk.Response{
			Message: "Failed to run interfaces report",
			Detail:  err.Error(),
		})
		return
	}

	httpapi.Write(r.Context(), rw, http.StatusOK, healthsdk.AgentNetcheckReport{
		BaseReport: healthsdk.BaseReport{
			Severity: health.SeverityOK,
		},
		NetInfo:    ni,
		Interfaces: ifReport,
	})
}
