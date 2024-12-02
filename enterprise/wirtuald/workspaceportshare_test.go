package coderd_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/wirtuald/coderdtest"
	"github.com/coder/coder/v2/wirtuald/rbac"
	"github.com/coder/coder/v2/wirtualsdk"
	"github.com/coder/coder/v2/enterprise/wirtuald/coderdenttest"
	"github.com/coder/coder/v2/enterprise/wirtuald/license"
	"github.com/coder/coder/v2/testutil"
)

func TestWorkspacePortShare(t *testing.T) {
	t.Parallel()

	ownerClient, owner := coderdenttest.New(t, &coderdenttest.Options{
		Options: &coderdtest.Options{
			IncludeProvisionerDaemon: true,
		},
		LicenseOptions: &coderdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureControlSharedPorts: 1,
			},
		},
	})
	client, user := coderdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID, rbac.RoleTemplateAdmin())
	r := setupWorkspaceAgent(t, client, wirtualsdk.CreateFirstUserResponse{
		UserID:         user.ID,
		OrganizationID: owner.OrganizationID,
	}, 0)
	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitShort)
	defer cancel()

	// try to update port share with template max port share level owner
	_, err := client.UpsertWorkspaceAgentPortShare(ctx, r.workspace.ID, wirtualsdk.UpsertWorkspaceAgentPortShareRequest{
		AgentName:  r.sdkAgent.Name,
		Port:       8080,
		ShareLevel: wirtualsdk.WorkspaceAgentPortShareLevelPublic,
		Protocol:   wirtualsdk.WorkspaceAgentPortShareProtocolHTTP,
	})
	require.Error(t, err, "Port sharing level not allowed")

	// update the template max port share level to public
	var level wirtualsdk.WorkspaceAgentPortShareLevel = wirtualsdk.WorkspaceAgentPortShareLevelPublic
	client.UpdateTemplateMeta(ctx, r.workspace.TemplateID, wirtualsdk.UpdateTemplateMeta{
		MaxPortShareLevel: &level,
	})

	// OK
	ps, err := client.UpsertWorkspaceAgentPortShare(ctx, r.workspace.ID, wirtualsdk.UpsertWorkspaceAgentPortShareRequest{
		AgentName:  r.sdkAgent.Name,
		Port:       8080,
		ShareLevel: wirtualsdk.WorkspaceAgentPortShareLevelPublic,
		Protocol:   wirtualsdk.WorkspaceAgentPortShareProtocolHTTP,
	})
	require.NoError(t, err)
	require.EqualValues(t, wirtualsdk.WorkspaceAgentPortShareLevelPublic, ps.ShareLevel)
}
