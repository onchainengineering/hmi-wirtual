package cli_test

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

// TestStart also tests restart since the tests are virtually identical.
func TestStart(t *testing.T) {
	t.Parallel()

	t.Run("RequireActiveVersion", func(t *testing.T) {
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
		templateAdminClient, templateAdmin := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID, rbac.RoleTemplateAdmin())

		// Create an initial version.
		oldVersion := wirtualdtest.CreateTemplateVersion(t, templateAdminClient, owner.OrganizationID, nil)
		// Create a template that mandates the promoted version.
		// This should be enforced for everyone except template admins.
		template := wirtualdtest.CreateTemplate(t, templateAdminClient, owner.OrganizationID, oldVersion.ID)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, templateAdminClient, oldVersion.ID)
		require.Equal(t, oldVersion.ID, template.ActiveVersionID)
		template = wirtualdtest.UpdateTemplateMeta(t, templateAdminClient, template.ID, wirtualsdk.UpdateTemplateMeta{
			RequireActiveVersion: true,
		})
		require.True(t, template.RequireActiveVersion)

		// Create a new version that we will promote.
		activeVersion := wirtualdtest.CreateTemplateVersion(t, templateAdminClient, owner.OrganizationID, nil, func(ctvr *wirtualsdk.CreateTemplateVersionRequest) {
			ctvr.TemplateID = template.ID
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, templateAdminClient, activeVersion.ID)
		err := templateAdminClient.UpdateActiveTemplateVersion(ctx, template.ID, wirtualsdk.UpdateActiveTemplateVersion{
			ID: activeVersion.ID,
		})
		require.NoError(t, err)

		templateACLAdminClient, templateACLAdmin := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID)
		templateGroupACLAdminClient, templateGroupACLAdmin := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID)
		memberClient, member := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID)

		// Create a group so we can also test group template admin ownership.
		// Add the user who gains template admin via group membership.
		group := wirtualdtest.CreateGroup(t, ownerClient, owner.OrganizationID, "test", templateGroupACLAdmin)

		// Update the template for both users and groups.
		err = ownerClient.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{
				templateACLAdmin.ID.String(): wirtualsdk.TemplateRoleAdmin,
			},
			GroupPerms: map[string]wirtualsdk.TemplateRole{
				group.ID.String(): wirtualsdk.TemplateRoleAdmin,
			},
		})
		require.NoError(t, err)

		type testcase struct {
			Name            string
			Client          *wirtualsdk.Client
			WorkspaceOwner  uuid.UUID
			ExpectedVersion uuid.UUID
		}

		cases := []testcase{
			{
				Name:            "OwnerUnchanged",
				Client:          ownerClient,
				WorkspaceOwner:  owner.UserID,
				ExpectedVersion: oldVersion.ID,
			},
			{
				Name:            "TemplateAdminUnchanged",
				Client:          templateAdminClient,
				WorkspaceOwner:  templateAdmin.ID,
				ExpectedVersion: oldVersion.ID,
			},
			{
				Name:            "TemplateACLAdminUnchanged",
				Client:          templateACLAdminClient,
				WorkspaceOwner:  templateACLAdmin.ID,
				ExpectedVersion: oldVersion.ID,
			},
			{
				Name:            "TemplateGroupACLAdminUnchanged",
				Client:          templateGroupACLAdminClient,
				WorkspaceOwner:  templateGroupACLAdmin.ID,
				ExpectedVersion: oldVersion.ID,
			},
			{
				Name:            "MemberUpdates",
				Client:          memberClient,
				WorkspaceOwner:  member.ID,
				ExpectedVersion: activeVersion.ID,
			},
		}

		for _, cmd := range []string{"start", "restart"} {
			cmd := cmd
			t.Run(cmd, func(t *testing.T) {
				t.Parallel()
				for _, c := range cases {
					c := c
					t.Run(c.Name, func(t *testing.T) {
						t.Parallel()

						// Instantiate a new context for each subtest since
						// they can potentially be lengthy.
						ctx := testutil.Context(t, testutil.WaitMedium)
						// Create the workspace using the admin since we want
						// to force the old version.
						ws, err := ownerClient.CreateWorkspace(ctx, owner.OrganizationID, c.WorkspaceOwner.String(), wirtualsdk.CreateWorkspaceRequest{
							TemplateVersionID: oldVersion.ID,
							Name:              wirtualdtest.RandomUsername(t),
							AutomaticUpdates:  wirtualsdk.AutomaticUpdatesNever,
						})
						require.NoError(t, err)
						wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, c.Client, ws.LatestBuild.ID)

						initialTemplateVersion := ws.LatestBuild.TemplateVersionID

						if cmd == "start" {
							// Stop the workspace so that we can start it.
							wirtualdtest.MustTransitionWorkspace(t, c.Client, ws.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop)
						}
						// Start the workspace. Every test permutation should
						// pass.
						var buf bytes.Buffer
						inv, conf := newCLI(t, cmd, ws.Name, "-y")
						inv.Stdout = &buf
						clitest.SetupConfig(t, c.Client, conf)
						err = inv.Run()
						require.NoError(t, err)

						ws = wirtualdtest.MustWorkspace(t, c.Client, ws.ID)
						require.Equal(t, c.ExpectedVersion, ws.LatestBuild.TemplateVersionID)
						if initialTemplateVersion == ws.LatestBuild.TemplateVersionID {
							return
						}

						if cmd == "start" {
							require.Contains(t, buf.String(), "Unable to start the workspace with the template version from the last build")
						}

						if cmd == "restart" {
							require.Contains(t, buf.String(), "Unable to restart the workspace with the template version from the last build")
						}
					})
				}
			})
		}
	})

	t.Run("StartActivatesDormant", func(t *testing.T) {
		t.Parallel()

		ctx := testutil.Context(t, testutil.WaitMedium)
		ownerClient, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAdvancedTemplateScheduling: 1,
				},
			},
		})

		version := wirtualdtest.CreateTemplateVersion(t, ownerClient, owner.OrganizationID, nil)
		_ = wirtualdtest.AwaitTemplateVersionJobCompleted(t, ownerClient, version.ID)
		template := wirtualdtest.CreateTemplate(t, ownerClient, owner.OrganizationID, version.ID)

		memberClient, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID)
		workspace := wirtualdtest.CreateWorkspace(t, memberClient, template.ID)
		_ = wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, memberClient, workspace.LatestBuild.ID)
		_ = wirtualdtest.MustTransitionWorkspace(t, memberClient, workspace.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop)
		err := memberClient.UpdateWorkspaceDormancy(ctx, workspace.ID, wirtualsdk.UpdateWorkspaceDormancy{
			Dormant: true,
		})
		require.NoError(t, err)

		inv, root := newCLI(t, "start", workspace.Name)
		clitest.SetupConfig(t, memberClient, root)

		var buf bytes.Buffer
		inv.Stdout = &buf

		err = inv.Run()
		require.NoError(t, err)
		require.Contains(t, buf.String(), "Activating dormant workspace...")

		workspace = wirtualdtest.MustWorkspace(t, memberClient, workspace.ID)
		require.Equal(t, wirtualsdk.WorkspaceTransitionStart, workspace.LatestBuild.Transition)
	})
}
