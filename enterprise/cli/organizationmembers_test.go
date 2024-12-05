package cli_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/cli/clitest"
	"github.com/coder/coder/v2/enterprise/wirtuald/license"
	"github.com/coder/coder/v2/enterprise/wirtuald/wirtualdenttest"
	"github.com/coder/coder/v2/testutil"
	"github.com/coder/coder/v2/wirtuald/rbac"
	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
	"github.com/coder/coder/v2/wirtualsdk"
)

func TestRemoveOrganizationMembers(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		ownerClient, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureMultipleOrganizations: 1,
				},
			},
		})

		secondOrganization := wirtualdenttest.CreateOrganization(t, ownerClient, wirtualdenttest.CreateOrganizationOptions{})
		orgAdminClient, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, secondOrganization.ID, rbac.ScopedRoleOrgAdmin(secondOrganization.ID))
		_, user := wirtualdtest.CreateAnotherUser(t, ownerClient, secondOrganization.ID)

		ctx := testutil.Context(t, testutil.WaitMedium)

		inv, root := clitest.New(t, "organization", "members", "remove", "-O", secondOrganization.ID.String(), user.Username)
		clitest.SetupConfig(t, orgAdminClient, root)

		buf := new(bytes.Buffer)
		inv.Stdout = buf
		err := inv.WithContext(ctx).Run()
		require.NoError(t, err)

		members, err := orgAdminClient.OrganizationMembers(ctx, secondOrganization.ID)
		require.NoError(t, err)

		require.Len(t, members, 2)
	})

	t.Run("UserNotExists", func(t *testing.T) {
		t.Parallel()

		ownerClient := wirtualdtest.New(t, &wirtualdtest.Options{})
		owner := wirtualdtest.CreateFirstUser(t, ownerClient)
		orgAdminClient, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID, rbac.ScopedRoleOrgAdmin(owner.OrganizationID))

		ctx := testutil.Context(t, testutil.WaitMedium)

		inv, root := clitest.New(t, "organization", "members", "remove", "-O", owner.OrganizationID.String(), "random_name")
		clitest.SetupConfig(t, orgAdminClient, root)

		buf := new(bytes.Buffer)
		inv.Stdout = buf
		err := inv.WithContext(ctx).Run()
		require.ErrorContains(t, err, "must be an existing uuid or username")
	})
}

func TestEnterpriseListOrganizationMembers(t *testing.T) {
	t.Parallel()

	t.Run("CustomRole", func(t *testing.T) {
		t.Parallel()

		ownerClient, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureCustomRoles: 1,
				},
			},
		})

		ctx := testutil.Context(t, testutil.WaitMedium)
		//nolint:gocritic // only owners can patch roles
		customRole, err := ownerClient.CreateOrganizationRole(ctx, wirtualsdk.Role{
			Name:            "custom",
			OrganizationID:  owner.OrganizationID.String(),
			DisplayName:     "Custom Role",
			SitePermissions: nil,
			OrganizationPermissions: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				wirtualsdk.ResourceWorkspace: {wirtualsdk.ActionRead},
			}),
			UserPermissions: nil,
		})
		require.NoError(t, err)

		client, user := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID, rbac.RoleUserAdmin(), rbac.RoleIdentifier{
			Name:           customRole.Name,
			OrganizationID: owner.OrganizationID,
		}, rbac.ScopedRoleOrgAdmin(owner.OrganizationID))

		inv, root := clitest.New(t, "organization", "members", "list", "-c", "user id,username,organization roles")
		clitest.SetupConfig(t, client, root)

		buf := new(bytes.Buffer)
		inv.Stdout = buf
		err = inv.WithContext(ctx).Run()
		require.NoError(t, err)
		require.Contains(t, buf.String(), user.Username)
		require.Contains(t, buf.String(), owner.UserID.String())
		// Check the display name is the value in the cli list
		require.Contains(t, buf.String(), customRole.DisplayName)
	})
}

func TestAssignOrganizationMemberRole(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()
		ownerClient, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureCustomRoles: 1,
				},
			},
		})
		_, user := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID, rbac.RoleUserAdmin())

		ctx := testutil.Context(t, testutil.WaitMedium)
		// nolint:gocritic // requires owner role to create
		customRole, err := ownerClient.CreateOrganizationRole(ctx, wirtualsdk.Role{
			Name:            "custom-role",
			OrganizationID:  owner.OrganizationID.String(),
			DisplayName:     "Custom Role",
			SitePermissions: nil,
			OrganizationPermissions: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				wirtualsdk.ResourceWorkspace: {wirtualsdk.ActionRead},
			}),
			UserPermissions: nil,
		})
		require.NoError(t, err)

		inv, root := clitest.New(t, "organization", "members", "edit-roles", user.Username, wirtualsdk.RoleOrganizationAdmin, customRole.Name)
		// nolint:gocritic // you cannot change your own roles
		clitest.SetupConfig(t, ownerClient, root)

		buf := new(bytes.Buffer)
		inv.Stdout = buf
		err = inv.WithContext(ctx).Run()
		require.NoError(t, err)
		require.Contains(t, buf.String(), must(rbac.RoleByName(rbac.ScopedRoleOrgAdmin(owner.OrganizationID))).DisplayName)
		require.Contains(t, buf.String(), customRole.DisplayName)
	})
}

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
