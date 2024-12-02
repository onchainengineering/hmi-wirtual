package wirtuald_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
	"github.com/coder/coder/v2/wirtuald/rbac"
	"github.com/coder/coder/v2/wirtualsdk"
	"github.com/coder/coder/v2/enterprise/wirtuald/wirtualdenttest"
	"github.com/coder/coder/v2/enterprise/wirtuald/license"
	"github.com/coder/coder/v2/testutil"
)

func TestWorkspaceBuild(t *testing.T) {
	t.Parallel()
	t.Run("TemplateRequiresActiveVersion", func(t *testing.T) {
		t.Parallel()

		ctx := testutil.Context(t, testutil.WaitMedium)
		ownerClient, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAccessControl:              1,
					wirtualsdk.FeatureTemplateRBAC:               1,
					wirtualsdk.FeatureAdvancedTemplateScheduling: 1,
				},
			},
		})

		// Create an initial version.
		oldVersion := wirtualdtest.CreateTemplateVersion(t, ownerClient, owner.OrganizationID, nil)
		// Create a template that mandates the promoted version.
		// This should be enforced for everyone except template admins.
		template := wirtualdtest.CreateTemplate(t, ownerClient, owner.OrganizationID, oldVersion.ID)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, ownerClient, oldVersion.ID)
		require.Equal(t, oldVersion.ID, template.ActiveVersionID)
		template = wirtualdtest.UpdateTemplateMeta(t, ownerClient, template.ID, wirtualsdk.UpdateTemplateMeta{
			RequireActiveVersion: true,
		})
		require.True(t, template.RequireActiveVersion)

		// Create a new version that we will promote.
		activeVersion := wirtualdtest.CreateTemplateVersion(t, ownerClient, owner.OrganizationID, nil, func(ctvr *wirtualsdk.CreateTemplateVersionRequest) {
			ctvr.TemplateID = template.ID
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, ownerClient, activeVersion.ID)
		wirtualdtest.UpdateActiveTemplateVersion(t, ownerClient, template.ID, activeVersion.ID)

		templateAdminClient, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID, rbac.RoleTemplateAdmin())
		templateACLAdminClient, templateACLAdmin := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID)
		templateGroupACLAdminClient, templateGroupACLAdmin := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID)
		memberClient, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID)

		// Create a group so we can also test group template admin ownership.
		// Add the user who gains template admin via group membership.
		group := wirtualdtest.CreateGroup(t, ownerClient, owner.OrganizationID, "test", templateGroupACLAdmin)

		// Update the template for both users and groups.
		//nolint:gocritic // test setup
		err := ownerClient.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				templateACLAdmin.ID.String(): wirtualsdk.TemplateRoleAdmin,
			},
			GroupPerms: map[string]wirtualsdk.TemplateRole{
				group.ID.String(): wirtualsdk.TemplateRoleAdmin,
			},
		})
		require.NoError(t, err)

		type testcase struct {
			Name               string
			Client             *wirtualsdk.Client
			ExpectedStatusCode int
		}

		cases := []testcase{
			{
				Name:               "OwnerOK",
				Client:             ownerClient,
				ExpectedStatusCode: http.StatusOK,
			},
			{
				Name:               "TemplateAdminOK",
				Client:             templateAdminClient,
				ExpectedStatusCode: http.StatusOK,
			},
			{
				Name:               "TemplateACLAdminOK",
				Client:             templateACLAdminClient,
				ExpectedStatusCode: http.StatusOK,
			},
			{
				Name:               "TemplateGroupACLAdminOK",
				Client:             templateGroupACLAdminClient,
				ExpectedStatusCode: http.StatusOK,
			},
			{
				Name:               "MemberFails",
				Client:             memberClient,
				ExpectedStatusCode: http.StatusForbidden,
			},
		}

		for _, c := range cases {
			t.Run(c.Name, func(t *testing.T) {
				_, err = c.Client.CreateWorkspace(ctx, owner.OrganizationID, wirtualsdk.Me, wirtualsdk.CreateWorkspaceRequest{
					TemplateVersionID: oldVersion.ID,
					Name:              "abc123",
					AutomaticUpdates:  wirtualsdk.AutomaticUpdatesNever,
				})
				if c.ExpectedStatusCode == http.StatusOK {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
					cerr, ok := wirtualsdk.AsError(err)
					require.True(t, ok)
					require.Equal(t, c.ExpectedStatusCode, cerr.StatusCode())
				}
			})
		}
	})
}
