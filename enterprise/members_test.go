package enterprise_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/db2sdk"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/onchainengineering/hmi-wirtual/testutil"
)

func TestEnterpriseMembers(t *testing.T) {
	t.Parallel()

	t.Run("Remove", func(t *testing.T) {
		t.Parallel()
		owner, first := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureMultipleOrganizations: 1,
					wirtualsdk.FeatureTemplateRBAC:          1,
				},
			},
		})

		secondOrg := wirtualdenttest.CreateOrganization(t, owner, wirtualdenttest.CreateOrganizationOptions{})

		orgAdminClient, orgAdmin := wirtualdtest.CreateAnotherUser(t, owner, secondOrg.ID, rbac.ScopedRoleOrgAdmin(secondOrg.ID))
		_, user := wirtualdtest.CreateAnotherUser(t, owner, secondOrg.ID)

		ctx := testutil.Context(t, testutil.WaitMedium)

		// Groups exist to ensure a user removed from the org loses their
		// group access.
		g1, err := orgAdminClient.CreateGroup(ctx, secondOrg.ID, wirtualsdk.CreateGroupRequest{
			Name:        "foo",
			DisplayName: "Foo",
		})
		require.NoError(t, err)

		g2, err := orgAdminClient.CreateGroup(ctx, secondOrg.ID, wirtualsdk.CreateGroupRequest{
			Name:        "bar",
			DisplayName: "Bar",
		})
		require.NoError(t, err)

		// Verify the org of 3 members
		members, err := orgAdminClient.OrganizationMembers(ctx, secondOrg.ID)
		require.NoError(t, err)
		require.Len(t, members, 3)
		require.ElementsMatch(t,
			[]uuid.UUID{first.UserID, user.ID, orgAdmin.ID},
			db2sdk.List(members, onlyIDs))

		// Add the member to some groups
		_, err = orgAdminClient.PatchGroup(ctx, g1.ID, wirtualsdk.PatchGroupRequest{
			AddUsers: []string{user.ID.String()},
		})
		require.NoError(t, err)

		_, err = orgAdminClient.PatchGroup(ctx, g2.ID, wirtualsdk.PatchGroupRequest{
			AddUsers: []string{user.ID.String()},
		})
		require.NoError(t, err)

		// Verify group membership
		userGroups, err := orgAdminClient.Groups(ctx, wirtualsdk.GroupArguments{
			HasMember: user.ID.String(),
		})
		require.NoError(t, err)
		// Everyone group + 2 groups
		require.Len(t, userGroups, 3)

		// Delete a member
		err = orgAdminClient.DeleteOrganizationMember(ctx, secondOrg.ID, user.Username)
		require.NoError(t, err)

		members, err = orgAdminClient.OrganizationMembers(ctx, secondOrg.ID)
		require.NoError(t, err)
		require.Len(t, members, 2)
		require.ElementsMatch(t,
			[]uuid.UUID{first.UserID, orgAdmin.ID},
			db2sdk.List(members, onlyIDs))

		// User should now belong to 0 groups
		userGroups, err = orgAdminClient.Groups(ctx, wirtualsdk.GroupArguments{
			HasMember: user.ID.String(),
		})
		require.NoError(t, err)
		require.Len(t, userGroups, 0)
	})

	t.Run("PostUser", func(t *testing.T) {
		t.Parallel()

		owner, first := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureMultipleOrganizations: 1,
				},
			},
		})

		ctx := testutil.Context(t, testutil.WaitMedium)
		org := wirtualdenttest.CreateOrganization(t, owner, wirtualdenttest.CreateOrganizationOptions{})

		// Make a user not in the second organization
		_, user := wirtualdtest.CreateAnotherUser(t, owner, first.OrganizationID)

		// Use scoped user admin in org to add the user
		client, userAdmin := wirtualdtest.CreateAnotherUser(t, owner, org.ID, rbac.ScopedRoleOrgUserAdmin(org.ID))

		members, err := client.OrganizationMembers(ctx, org.ID)
		require.NoError(t, err)
		require.Len(t, members, 2) // Verify the 2 members at the start

		// Add user to org
		_, err = client.PostOrganizationMember(ctx, org.ID, user.Username)
		require.NoError(t, err)

		members, err = client.OrganizationMembers(ctx, org.ID)
		require.NoError(t, err)
		// Owner + user admin + new member
		require.Len(t, members, 3)
		require.ElementsMatch(t,
			[]uuid.UUID{first.UserID, user.ID, userAdmin.ID},
			db2sdk.List(members, onlyIDs))
	})

	t.Run("PostUserNotExists", func(t *testing.T) {
		t.Parallel()
		owner, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureMultipleOrganizations: 1,
				},
			},
		})

		org := wirtualdenttest.CreateOrganization(t, owner, wirtualdenttest.CreateOrganizationOptions{})

		ctx := testutil.Context(t, testutil.WaitMedium)
		// Add user to org
		//nolint:gocritic // Using owner to ensure it's not a 404 error
		_, err := owner.PostOrganizationMember(ctx, org.ID, uuid.NewString())
		require.Error(t, err)
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Contains(t, apiErr.Message, "must be an existing")
	})

	// Calling it from a user without the org access.
	t.Run("ListNotInOrg", func(t *testing.T) {
		t.Parallel()

		owner, first := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureMultipleOrganizations: 1,
				},
			},
		})

		client, _ := wirtualdtest.CreateAnotherUser(t, owner, first.OrganizationID, rbac.ScopedRoleOrgAdmin(first.OrganizationID))
		org := wirtualdenttest.CreateOrganization(t, owner, wirtualdenttest.CreateOrganizationOptions{})

		ctx := testutil.Context(t, testutil.WaitShort)

		// 404 error is expected instead of a 403/401 to not leak existence of
		// an organization.
		_, err := client.OrganizationMembers(ctx, org.ID)
		require.ErrorContains(t, err, "404")
	})
}

func onlyIDs(u wirtualsdk.OrganizationMemberWithUserData) uuid.UUID {
	return u.UserID
}
