package wirtuald_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestCheckACLPermissions(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	t.Cleanup(cancel)

	adminClient, adminUser := wirtualdenttest.New(t, &wirtualdenttest.Options{
		Options: &wirtualdtest.Options{
			IncludeProvisionerDaemon: true,
		},
		LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		},
	})
	// Create member and org adminClient
	memberClient, _ := wirtualdtest.CreateAnotherUser(t, adminClient, adminUser.OrganizationID)
	memberUser, err := memberClient.User(ctx, wirtualsdk.Me)
	require.NoError(t, err)
	orgAdminClient, _ := wirtualdtest.CreateAnotherUser(t, adminClient, adminUser.OrganizationID, rbac.ScopedRoleOrgAdmin(adminUser.OrganizationID))
	orgAdminUser, err := orgAdminClient.User(ctx, wirtualsdk.Me)
	require.NoError(t, err)

	version := wirtualdtest.CreateTemplateVersion(t, adminClient, adminUser.OrganizationID, nil)
	wirtualdtest.AwaitTemplateVersionJobCompleted(t, adminClient, version.ID)
	template := wirtualdtest.CreateTemplate(t, adminClient, adminUser.OrganizationID, version.ID)

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
