package wirtuald_test

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbgen"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac/policy"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestListCustomRoles(t *testing.T) {
	t.Parallel()

	t.Run("Organizations", func(t *testing.T) {
		t.Parallel()

		client, db := wirtualdtest.NewWithDatabase(t, nil)
		owner := wirtualdtest.CreateFirstUser(t, client)

		const roleName = "random_role"
		dbgen.CustomRole(t, db, database.CustomRole{
			Name:        roleName,
			DisplayName: "Random Role",
			OrganizationID: uuid.NullUUID{
				UUID:  owner.OrganizationID,
				Valid: true,
			},
			SitePermissions: nil,
			OrgPermissions: []database.CustomRolePermission{
				{
					Negate:       false,
					ResourceType: rbac.ResourceWorkspace.Type,
					Action:       policy.ActionRead,
				},
			},
			UserPermissions: nil,
		})

		ctx := testutil.Context(t, testutil.WaitShort)
		roles, err := client.ListOrganizationRoles(ctx, owner.OrganizationID)
		require.NoError(t, err)

		found := slices.ContainsFunc(roles, func(element wirtualsdk.AssignableRoles) bool {
			return element.Name == roleName && element.OrganizationID == owner.OrganizationID.String()
		})
		require.Truef(t, found, "custom organization role listed")
	})
}
