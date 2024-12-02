package wirtuald_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
	"github.com/coder/coder/v2/wirtuald/rbac"
	"github.com/coder/coder/v2/wirtualsdk"
	"github.com/coder/coder/v2/testutil"
)

func TestCheckPermissions(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	t.Cleanup(cancel)

	adminClient := wirtualdtest.New(t, &wirtualdtest.Options{
		IncludeProvisionerDaemon: true,
	})
	// Create adminClient, member, and org adminClient
	adminUser := wirtualdtest.CreateFirstUser(t, adminClient)
	memberClient, _ := wirtualdtest.CreateAnotherUser(t, adminClient, adminUser.OrganizationID)
	memberUser, err := memberClient.User(ctx, wirtualsdk.Me)
	require.NoError(t, err)
	orgAdminClient, _ := wirtualdtest.CreateAnotherUser(t, adminClient, adminUser.OrganizationID, rbac.ScopedRoleOrgAdmin(adminUser.OrganizationID))
	orgAdminUser, err := orgAdminClient.User(ctx, wirtualsdk.Me)
	require.NoError(t, err)

	version := wirtualdtest.CreateTemplateVersion(t, adminClient, adminUser.OrganizationID, nil)
	wirtualdtest.AwaitTemplateVersionJobCompleted(t, adminClient, version.ID)
	template := wirtualdtest.CreateTemplate(t, adminClient, adminUser.OrganizationID, version.ID)

	// With admin, member, and org admin
	const (
		readAllUsers           = "read-all-users"
		readOrgWorkspaces      = "read-org-workspaces"
		readMyself             = "read-myself"
		readOwnWorkspaces      = "read-own-workspaces"
		updateSpecificTemplate = "update-specific-template"
	)
	params := map[string]wirtualsdk.AuthorizationCheck{
		readAllUsers: {
			Object: wirtualsdk.AuthorizationObject{
				ResourceType: wirtualsdk.ResourceUser,
			},
			Action: "read",
		},
		readMyself: {
			Object: wirtualsdk.AuthorizationObject{
				ResourceType: wirtualsdk.ResourceUser,
				OwnerID:      "me",
			},
			Action: "read",
		},
		readOwnWorkspaces: {
			Object: wirtualsdk.AuthorizationObject{
				ResourceType: wirtualsdk.ResourceWorkspace,
				OwnerID:      "me",
			},
			Action: "read",
		},
		readOrgWorkspaces: {
			Object: wirtualsdk.AuthorizationObject{
				ResourceType:   wirtualsdk.ResourceWorkspace,
				OrganizationID: adminUser.OrganizationID.String(),
			},
			Action: "read",
		},
		updateSpecificTemplate: {
			Object: wirtualsdk.AuthorizationObject{
				ResourceType: wirtualsdk.ResourceTemplate,
				ResourceID:   template.ID.String(),
			},
			Action: "update",
		},
	}

	testCases := []struct {
		Name   string
		Client *wirtualsdk.Client
		UserID uuid.UUID
		Check  wirtualsdk.AuthorizationResponse
	}{
		{
			Name:   "Admin",
			Client: adminClient,
			UserID: adminUser.UserID,
			Check: map[string]bool{
				readAllUsers:           true,
				readMyself:             true,
				readOwnWorkspaces:      true,
				readOrgWorkspaces:      true,
				updateSpecificTemplate: true,
			},
		},
		{
			Name:   "OrgAdmin",
			Client: orgAdminClient,
			UserID: orgAdminUser.ID,
			Check: map[string]bool{
				readAllUsers:           true,
				readMyself:             true,
				readOwnWorkspaces:      true,
				readOrgWorkspaces:      true,
				updateSpecificTemplate: true,
			},
		},
		{
			Name:   "Member",
			Client: memberClient,
			UserID: memberUser.ID,
			Check: map[string]bool{
				readAllUsers:           false,
				readMyself:             true,
				readOwnWorkspaces:      true,
				readOrgWorkspaces:      false,
				updateSpecificTemplate: false,
			},
		},
	}

	for _, c := range testCases {
		c := c

		t.Run("CheckAuthorization/"+c.Name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			t.Cleanup(cancel)

			resp, err := c.Client.AuthCheck(ctx, wirtualsdk.AuthorizationRequest{Checks: params})
			require.NoError(t, err, "check perms")
			require.Equal(t, c.Check, resp)
		})
	}
}
