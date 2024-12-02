package proto

import (
	"github.com/onchainengineering/hmi-wirtual/tailnet/proto"
)

// CurrentVersion is the current version of the agent API.  It is tied to the
// tailnet API version to avoid confusion, since agents connect to the tailnet
// API over the same websocket.
var CurrentVersion = proto.CurrentVersion
