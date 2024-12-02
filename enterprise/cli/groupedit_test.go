package cli_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/pretty"

	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/cli/cliui"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/onchainengineering/hmi-wirtual/pty/ptytest"
)

func TestGroupEdit(t *testing.T) {
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
		_, user3 := wirtualdtest.CreateAnotherUser(t, client, admin.OrganizationID)

		group := wirtualdtest.CreateGroup(t, client, admin.OrganizationID, "alpha", user3)

		expectedName := "beta"

		inv, conf := newCLI(
			t,
			"groups", "edit", group.Name,
			"--name", expectedName,
			"--avatar-url", "https://example.com",
			"-a", user1.ID.String(),
			"-a", user2.Email,
			"-r", user3.ID.String(),
		)

		pty := ptytest.New(t)

		inv.Stdout = pty.Output()
		clitest.SetupConfig(t, anotherClient, conf)

		err := inv.Run()
		require.NoError(t, err)

		pty.ExpectMatch(fmt.Sprintf("Successfully patched group %s", pretty.Sprint(cliui.DefaultStyles.Keyword, expectedName)))
	})

	t.Run("InvalidUserInput", func(t *testing.T) {
		t.Parallel()

		client, admin := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})

		// Create a group with no members.
		group := wirtualdtest.CreateGroup(t, client, admin.OrganizationID, "alpha")

		inv, conf := newCLI(
			t,
			"groups", "edit", group.Name,
			"-a", "foo",
		)

		clitest.SetupConfig(t, client, conf) //nolint:gocritic // intentional usage of owner

		err := inv.Run()
		require.ErrorContains(t, err, "must be a valid UUID or email address")
	})

	t.Run("NoArg", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleUserAdmin())

		inv, conf := newCLI(t, "groups", "edit")
		clitest.SetupConfig(t, anotherClient, conf)

		err := inv.Run()
		require.ErrorContains(t, err, "wanted 1 args but got 0")
	})
}
