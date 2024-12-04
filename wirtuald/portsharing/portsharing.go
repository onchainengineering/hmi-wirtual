package portsharing

import (
	"golang.org/x/xerrors"

	"github.com/coder/coder/v2/wirtuald/database"
	"github.com/coder/coder/v2/wirtualsdk"
)

type PortSharer interface {
	AuthorizedLevel(template database.Template, level wirtualsdk.WorkspaceAgentPortShareLevel) error
	ValidateTemplateMaxLevel(level wirtualsdk.WorkspaceAgentPortShareLevel) error
	ConvertMaxLevel(level database.AppSharingLevel) wirtualsdk.WorkspaceAgentPortShareLevel
}

type AGPLPortSharer struct{}

func (AGPLPortSharer) AuthorizedLevel(_ database.Template, _ wirtualsdk.WorkspaceAgentPortShareLevel) error {
	return nil
}

func (AGPLPortSharer) ValidateTemplateMaxLevel(_ wirtualsdk.WorkspaceAgentPortShareLevel) error {
	return xerrors.New("Restricting port sharing level is an enterprise feature that is not enabled.")
}

func (AGPLPortSharer) ConvertMaxLevel(_ database.AppSharingLevel) wirtualsdk.WorkspaceAgentPortShareLevel {
	return wirtualsdk.WorkspaceAgentPortShareLevelPublic
}

var DefaultPortSharer PortSharer = AGPLPortSharer{}
