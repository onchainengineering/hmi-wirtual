package cli_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/pretty"

	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/cli/cliui"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/pty/ptytest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestGroupDelete(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		client, admin := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, admin.OrganizationID, rbac.RoleUserAdmin())

		group := wirtualdtest.CreateGroup(t, client, admin.OrganizationID, "alpha")

		inv, conf := newCLI(t,
			"groups", "delete", group.Name,
		)

		pty := ptytest.New(t)

		inv.Stdout = pty.Output()
		clitest.SetupConfig(t, anotherClient, conf)

		err := inv.Run()
		require.NoError(t, err)

		pty.ExpectMatch(fmt.Sprintf("Successfully deleted group %s", pretty.Sprint(cliui.DefaultStyles.Keyword, group.Name)))
	})

	t.Run("NoArg", func(t *testing.T) {
		t.Parallel()

		client, admin := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, admin.OrganizationID, rbac.RoleUserAdmin())

		inv, conf := newCLI(
			t,
			"groups", "delete",
		)

		clitest.SetupConfig(t, anotherClient, conf)

		err := inv.Run()
		require.Error(t, err)
	})
}
