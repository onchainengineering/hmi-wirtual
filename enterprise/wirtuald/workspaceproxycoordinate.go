package wirtuald

import (
	"net/http"

	"github.com/google/uuid"
	"nhooyr.io/websocket"

	"github.com/onchainengineering/hmi-wirtual/apiversion"
	"github.com/onchainengineering/hmi-wirtual/tailnet/proto"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/httpapi"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

// @Summary Workspace Proxy Coordinate
// @ID workspace-proxy-coordinate
// @Security CoderSessionToken
// @Tags Enterprise
// @Success 101
// @Router /workspaceproxies/me/coordinate [get]
// @x-apidocgen {"skip": true}
func (api *API) workspaceProxyCoordinate(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	version := "1.0"
	msgType := websocket.MessageText
	qv := r.URL.Query().Get("version")
	if qv != "" {
		version = qv
	}
	if err := proto.CurrentVersion.Validate(version); err != nil {
		httpapi.Write(ctx, rw, http.StatusBadRequest, wirtualsdk.Response{
			Message: "Unknown or unsupported API version",
			Validations: []wirtualsdk.ValidationError{
				{Field: "version", Detail: err.Error()},
			},
		})
		return
	}
	maj, _, _ := apiversion.Parse(version)
	if maj >= 2 {
		// Versions 2+ use dRPC over a binary connection
		msgType = websocket.MessageBinary
	}

	api.AGPL.WebsocketWaitMutex.Lock()
	api.AGPL.WebsocketWaitGroup.Add(1)
	api.AGPL.WebsocketWaitMutex.Unlock()
	defer api.AGPL.WebsocketWaitGroup.Done()

	conn, err := websocket.Accept(rw, r, nil)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusBadRequest, wirtualsdk.Response{
			Message: "Failed to accept websocket.",
			Detail:  err.Error(),
		})
		return
	}

	ctx, nc := wirtualsdk.WebsocketNetConn(ctx, conn, msgType)
	defer nc.Close()

	id := uuid.New()
	err = api.tailnetService.ServeMultiAgentClient(ctx, version, nc, id)
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, err.Error())
	} else {
		_ = conn.Close(websocket.StatusGoingAway, "")
	}
}
