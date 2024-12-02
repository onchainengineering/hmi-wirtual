package portsharing

import (
	"golang.org/x/xerrors"

	"github.com/coder/coder/v2/wirtuald/database"
	"github.com/coder/coder/v2/wirtualsdk"
)

type EnterprisePortSharer struct{}

func NewEnterprisePortSharer() *EnterprisePortSharer {
	return &EnterprisePortSharer{}
}

func (EnterprisePortSharer) AuthorizedLevel(template database.Template, level wirtualsdk.WorkspaceAgentPortShareLevel) error {
	max := wirtualsdk.WorkspaceAgentPortShareLevel(template.MaxPortSharingLevel)
	switch level {
	case wirtualsdk.WorkspaceAgentPortShareLevelPublic:
		if max != wirtualsdk.WorkspaceAgentPortShareLevelPublic {
			return xerrors.Errorf("port sharing level not allowed. Max level is '%s'", max)
		}
	case wirtualsdk.WorkspaceAgentPortShareLevelAuthenticated:
		if max == wirtualsdk.WorkspaceAgentPortShareLevelOwner {
			return xerrors.Errorf("port sharing level not allowed. Max level is '%s'", max)
		}
	default:
		return xerrors.New("port sharing level is invalid.")
	}

	return nil
}

func (EnterprisePortSharer) ValidateTemplateMaxLevel(level wirtualsdk.WorkspaceAgentPortShareLevel) error {
	if !level.ValidMaxLevel() {
		return xerrors.New("invalid max port sharing level, value must be 'authenticated' or 'public'.")
	}

	return nil
}

func (EnterprisePortSharer) ConvertMaxLevel(level database.AppSharingLevel) wirtualsdk.WorkspaceAgentPortShareLevel {
	return wirtualsdk.WorkspaceAgentPortShareLevel(level)
}
