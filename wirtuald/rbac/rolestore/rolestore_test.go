package rolestore_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/testutil"
	"github.com/coder/coder/v2/wirtuald/database"
	"github.com/coder/coder/v2/wirtuald/database/dbgen"
	"github.com/coder/coder/v2/wirtuald/database/dbmem"
	"github.com/coder/coder/v2/wirtuald/rbac"
	"github.com/coder/coder/v2/wirtuald/rbac/rolestore"
)

func TestExpandCustomRoleRoles(t *testing.T) {
	t.Parallel()

	db := dbmem.New()

	org := dbgen.Organization(t, db, database.Organization{})

	const roleName = "test-role"
	dbgen.CustomRole(t, db, database.CustomRole{
		Name:            roleName,
		DisplayName:     "",
		SitePermissions: nil,
		OrgPermissions:  nil,
		UserPermissions: nil,
		OrganizationID: uuid.NullUUID{
			UUID:  org.ID,
			Valid: true,
		},
	})

	ctx := testutil.Context(t, testutil.WaitShort)
	roles, err := rolestore.Expand(ctx, db, []rbac.RoleIdentifier{{Name: roleName, OrganizationID: org.ID}})
	require.NoError(t, err)
	require.Len(t, roles, 1, "role found")
}
