package coderd_test

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/testutil"
	"github.com/coder/coder/v2/wirtuald/coderdtest"
	"github.com/coder/coder/v2/wirtuald/database"
	"github.com/coder/coder/v2/wirtuald/database/dbgen"
	"github.com/coder/coder/v2/wirtuald/rbac"
	"github.com/coder/coder/v2/wirtuald/rbac/policy"
	"github.com/coder/coder/v2/wirtualsdk"
)

func TestListCustomRoles(t *testing.T) {
	t.Parallel()

	t.Run("Organizations", func(t *testing.T) {
		t.Parallel()

		client, db := coderdtest.NewWithDatabase(t, nil)
		owner := coderdtest.CreateFirstUser(t, client)

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
