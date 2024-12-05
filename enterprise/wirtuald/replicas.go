package wirtuald

import (
	"net/http"

	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/httpapi"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac/policy"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

// replicas returns the number of replicas that are active in Coder.
//
// @Summary Get active replicas
// @ID get-active-replicas
// @Security CoderSessionToken
// @Produce json
// @Tags Enterprise
// @Success 200 {array} wirtualsdk.Replica
// @Router /replicas [get]
func (api *API) replicas(rw http.ResponseWriter, r *http.Request) {
	if !api.AGPL.Authorize(r, policy.ActionRead, rbac.ResourceReplicas) {
		httpapi.ResourceNotFound(rw)
		return
	}

	replicas := api.replicaManager.AllPrimary()
	res := make([]wirtualsdk.Replica, 0, len(replicas))
	for _, replica := range replicas {
		res = append(res, convertReplica(replica))
	}
	httpapi.Write(r.Context(), rw, http.StatusOK, res)
}

func convertReplica(replica database.Replica) wirtualsdk.Replica {
	return wirtualsdk.Replica{
		ID:              replica.ID,
		Hostname:        replica.Hostname,
		CreatedAt:       replica.CreatedAt,
		RelayAddress:    replica.RelayAddress,
		RegionID:        replica.RegionID,
		Error:           replica.Error,
		DatabaseLatency: replica.DatabaseLatency,
	}
}
