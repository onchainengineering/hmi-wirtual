package cli_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
)

func TestListOrganizationMembers(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		ownerClient := wirtualdtest.New(t, &wirtualdtest.Options{})
		owner := wirtualdtest.CreateFirstUser(t, ownerClient)
		client, user := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID, rbac.RoleUserAdmin())

		ctx := testutil.Context(t, testutil.WaitMedium)
		inv, root := clitest.New(t, "organization", "members", "list", "-c", "user id,username,organization roles")
		clitest.SetupConfig(t, client, root)

		buf := new(bytes.Buffer)
		inv.Stdout = buf
		err := inv.WithContext(ctx).Run()
		require.NoError(t, err)
		require.Contains(t, buf.String(), user.Username)
		require.Contains(t, buf.String(), owner.UserID.String())
	})
}
