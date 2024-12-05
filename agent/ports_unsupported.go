//go:build !linux && !(windows && amd64)

package agent

import "github.com/onchainengineering/hmi-wirtual/wirtualsdk"

func (*listeningPortsHandler) getListeningPorts() ([]wirtualsdk.WorkspaceAgentListeningPort, error) {
	// Can't scan for ports on non-linux or non-windows_amd64 systems at the
	// moment. The UI will not show any "no ports found" message to the user, so
	// the user won't suspect a thing.
	return []wirtualsdk.WorkspaceAgentListeningPort{}, nil
}
