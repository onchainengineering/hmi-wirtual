package agent

import (
	"net/http"

	"github.com/onchainengineering/hmi-wirtual/wirtuald/healthcheck/health"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/httpapi"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk/healthsdk"
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
