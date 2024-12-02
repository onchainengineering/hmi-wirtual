package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/onchainengineering/hmi-wirtual/pty/ptytest"
)

func TestGroupList(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		client, admin := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, admin.OrganizationID, rbac.RoleUserAdmin())

		_, user1 := wirtualdtest.CreateAnotherUser(t, client, admin.OrganizationID)
		_, user2 := wirtualdtest.CreateAnotherUser(t, client, admin.OrganizationID)

		// We intentionally create the first group as beta so that we
		// can assert that things are being sorted by name intentionally
		// and not by chance (or some other parameter like created_at).
		group1 := wirtualdtest.CreateGroup(t, client, admin.OrganizationID, "beta", user1)
		group2 := wirtualdtest.CreateGroup(t, client, admin.OrganizationID, "alpha", user2)

		inv, conf := newCLI(t, "groups", "list")

		pty := ptytest.New(t)

		inv.Stdout = pty.Output()
		clitest.SetupConfig(t, anotherClient, conf)

		err := inv.Run()
		require.NoError(t, err)

		matches := []string{
			"NAME", "ORGANIZATION ID", "MEMBERS", " AVATAR URL",
			group2.Name, group2.OrganizationID.String(), user2.Email, group2.AvatarURL,
			group1.Name, group1.OrganizationID.String(), user1.Email, group1.AvatarURL,
		}

		for _, match := range matches {
			pty.ExpectMatch(match)
		}
	})

	t.Run("Everyone", func(t *testing.T) {
		t.Parallel()

		client, admin := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, admin.OrganizationID, rbac.RoleUserAdmin())

		inv, conf := newCLI(t, "groups", "list")

		pty := ptytest.New(t)

		inv.Stdout = pty.Output()
		clitest.SetupConfig(t, anotherClient, conf)

		err := inv.Run()
		require.NoError(t, err)

		matches := []string{
			"NAME", "ORGANIZATION ID", "MEMBERS", " AVATAR URL",
			"Everyone", admin.OrganizationID.String(), wirtualdtest.FirstUserParams.Email, "",
		}

		for _, match := range matches {
			pty.ExpectMatch(match)
		}
	})
}
