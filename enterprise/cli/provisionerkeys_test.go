package cli_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/cli/clitest"
	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
	"github.com/coder/coder/v2/wirtuald/provisionerkey"
	"github.com/coder/coder/v2/wirtuald/rbac"
	"github.com/coder/coder/v2/wirtualsdk"
	"github.com/coder/coder/v2/enterprise/wirtuald/wirtualdenttest"
	"github.com/coder/coder/v2/enterprise/wirtuald/license"
	"github.com/coder/coder/v2/pty/ptytest"
	"github.com/coder/coder/v2/testutil"
)

func TestProvisionerKeys(t *testing.T) {
	t.Parallel()

	t.Run("CRUD", func(t *testing.T) {
		t.Parallel()

		client, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureExternalProvisionerDaemons: 1,
				},
			},
		})
		orgAdminClient, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID, rbac.ScopedRoleOrgAdmin(owner.OrganizationID))

		name := "dont-TEST-me"
		ctx := testutil.Context(t, testutil.WaitMedium)
		inv, conf := newCLI(
			t,
			"provisioner", "keys", "create", name, "--tag", "foo=bar", "--tag", "my=way",
		)

		pty := ptytest.New(t)
		inv.Stdout = pty.Output()
		clitest.SetupConfig(t, orgAdminClient, conf)

		err := inv.WithContext(ctx).Run()
		require.NoError(t, err)

		line := pty.ReadLine(ctx)
		require.Contains(t, line, "Successfully created provisioner key")
		require.Contains(t, line, strings.ToLower(name))
		// empty line
		_ = pty.ReadLine(ctx)
		key := pty.ReadLine(ctx)
		require.NotEmpty(t, key)
		require.NoError(t, provisionerkey.Validate(key))

		inv, conf = newCLI(
			t,
			"provisioner", "keys", "ls",
		)
		pty = ptytest.New(t)
		inv.Stdout = pty.Output()
		clitest.SetupConfig(t, orgAdminClient, conf)

		err = inv.WithContext(ctx).Run()
		require.NoError(t, err)
		line = pty.ReadLine(ctx)
		require.Contains(t, line, "NAME")
		require.Contains(t, line, "CREATED AT")
		require.Contains(t, line, "TAGS")
		line = pty.ReadLine(ctx)
		require.Contains(t, line, strings.ToLower(name))
		require.Contains(t, line, "foo=bar my=way")

		inv, conf = newCLI(
			t,
			"provisioner", "keys", "delete", "-y", name,
		)

		pty = ptytest.New(t)
		inv.Stdout = pty.Output()
		clitest.SetupConfig(t, orgAdminClient, conf)

		err = inv.WithContext(ctx).Run()
		require.NoError(t, err)
		line = pty.ReadLine(ctx)
		require.Contains(t, line, "Successfully deleted provisioner key")
		require.Contains(t, line, strings.ToLower(name))

		inv, conf = newCLI(
			t,
			"provisioner", "keys", "ls",
		)
		pty = ptytest.New(t)
		inv.Stdout = pty.Output()
		clitest.SetupConfig(t, orgAdminClient, conf)

		err = inv.WithContext(ctx).Run()
		require.NoError(t, err)
		line = pty.ReadLine(ctx)
		require.Contains(t, line, "No provisioner keys found")
	})
}
