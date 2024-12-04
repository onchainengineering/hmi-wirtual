package coderd_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/coderd/coderdtest"
	"github.com/coder/coder/v2/coderd/rbac"
	"github.com/coder/coder/v2/enterprise/coderd/coderdenttest"
	"github.com/coder/coder/v2/enterprise/coderd/license"
	"github.com/coder/coder/v2/testutil"
	"github.com/coder/coder/v2/wirtualsdk"
)

func TestCheckACLPermissions(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	t.Cleanup(cancel)

	adminClient, adminUser := coderdenttest.New(t, &coderdenttest.Options{
		Options: &coderdtest.Options{
			IncludeProvisionerDaemon: true,
		},
		LicenseOptions: &coderdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		},
	})
	// Create member and org adminClient
	memberClient, _ := coderdtest.CreateAnotherUser(t, adminClient, adminUser.OrganizationID)
	memberUser, err := memberClient.User(ctx, wirtualsdk.Me)
	require.NoError(t, err)
	orgAdminClient, _ := coderdtest.CreateAnotherUser(t, adminClient, adminUser.OrganizationID, rbac.ScopedRoleOrgAdmin(adminUser.OrganizationID))
	orgAdminUser, err := orgAdminClient.User(ctx, wirtualsdk.Me)
	require.NoError(t, err)

	version := coderdtest.CreateTemplateVersion(t, adminClient, adminUser.OrganizationID, nil)
	coderdtest.AwaitTemplateVersionJobCompleted(t, adminClient, version.ID)
	template := coderdtest.CreateTemplate(t, adminClient, adminUser.OrganizationID, version.ID)

	err = adminClient.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
		UserPerms: map[string]wirtualsdk.TemplateRole{
			memberUser.ID.String(): wirtualsdk.TemplateRoleAdmin,
		},
	})
	require.NoError(t, err)

	const (
		updateSpecificTemplate = "read-specific-template"
	)
	params := map[string]wirtualsdk.AuthorizationCheck{
		updateSpecificTemplate: {
			Object: wirtualsdk.AuthorizationObject{
				ResourceType: wirtualsdk.ResourceTemplate,
				ResourceID:   template.ID.String(),
			},
			Action: wirtualsdk.ActionUpdate,
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
				updateSpecificTemplate: true,
			},
		},
		{
			Name:   "OrgAdmin",
			Client: orgAdminClient,
			UserID: orgAdminUser.ID,
			Check: map[string]bool{
				updateSpecificTemplate: true,
			},
		},
		{
			Name:   "Member",
			Client: memberClient,
			UserID: memberUser.ID,
			Check: map[string]bool{
				updateSpecificTemplate: true,
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
