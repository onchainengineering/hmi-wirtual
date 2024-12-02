package provisionerd

import (
	"context"

	"golang.org/x/xerrors"

	"github.com/onchainengineering/hmi-wirtual/provisionerd/proto"

	sdkproto "github.com/onchainengineering/hmi-wirtual/provisionersdk/proto"
)

// LocalProvisioners is a Connector that stores a static set of in-process
// provisioners.
type LocalProvisioners map[string]sdkproto.DRPCProvisionerClient

func (l LocalProvisioners) Connect(_ context.Context, job *proto.AcquiredJob, respCh chan<- ConnectResponse) {
	r := ConnectResponse{Job: job}
	p, ok := l[job.Provisioner]
	if ok {
		r.Client = p
	} else {
		r.Error = xerrors.Errorf("missing provisioner type %s", job.Provisioner)
	}
	go func() {
		respCh <- r
	}()
}
