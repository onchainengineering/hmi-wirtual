package cli_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/pty/ptytest"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestEditOrganizationRoles(t *testing.T) {
	t.Parallel()

	// Unit test uses --stdin and json as the role input. The interactive cli would
	// be hard to drive from a unit test.
	t.Run("JSON", func(t *testing.T) {
		t.Parallel()

		client, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureCustomRoles: 1,
				},
			},
		})

		ctx := testutil.Context(t, testutil.WaitMedium)
		inv, root := clitest.New(t, "organization", "roles", "edit", "--stdin")
		inv.Stdin = bytes.NewBufferString(fmt.Sprintf(`{
    "name": "new-role",
    "organization_id": "%s",
    "display_name": "",
    "site_permissions": [],
    "organization_permissions": [
		{
		  "resource_type": "workspace",
		  "action": "read"
		}
    ],
    "user_permissions": [],
    "assignable": false,
    "built_in": false
  }`, owner.OrganizationID.String()))
		//nolint:gocritic // only owners can edit roles
		clitest.SetupConfig(t, client, root)

		buf := new(bytes.Buffer)
		inv.Stdout = buf
		err := inv.WithContext(ctx).Run()
		require.NoError(t, err)
		require.Contains(t, buf.String(), "new-role")
	})

	t.Run("InvalidRole", func(t *testing.T) {
		t.Parallel()

		client, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureCustomRoles: 1,
				},
			},
		})

		ctx := testutil.Context(t, testutil.WaitMedium)
		inv, root := clitest.New(t, "organization", "roles", "edit", "--stdin")
		inv.Stdin = bytes.NewBufferString(fmt.Sprintf(`{
    "name": "new-role",
    "organization_id": "%s",
    "display_name": "",
    "site_permissions": [
		{
		  "resource_type": "workspace",
		  "action": "read"
		}
	],
    "organization_permissions": [
		{
		  "resource_type": "workspace",
		  "action": "read"
		}
    ],
    "user_permissions": [],
    "assignable": false,
    "built_in": false
  }`, owner.OrganizationID.String()))
		//nolint:gocritic // only owners can edit roles
		clitest.SetupConfig(t, client, root)

		buf := new(bytes.Buffer)
		inv.Stdout = buf
		err := inv.WithContext(ctx).Run()
		require.ErrorContains(t, err, "not allowed to assign site wide permissions for an organization role")
	})
}

func TestShowOrganizations(t *testing.T) {
	t.Parallel()

	t.Run("OnlyID", func(t *testing.T) {
		t.Parallel()

		ownerClient, first := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureMultipleOrganizations:      1,
					wirtualsdk.FeatureExternalProvisionerDaemons: 1,
				},
			},
		})

		// Owner is required to make orgs
		client, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, first.OrganizationID, rbac.RoleOwner())

		ctx := testutil.Context(t, testutil.WaitMedium)
		orgs := []string{"foo", "bar"}
		for _, orgName := range orgs {
			_, err := client.CreateOrganization(ctx, wirtualsdk.CreateOrganizationRequest{
				Name: orgName,
			})
			require.NoError(t, err)
		}

		inv, root := clitest.New(t, "organizations", "show", "--only-id", "--org="+first.OrganizationID.String())
		clitest.SetupConfig(t, client, root)
		pty := ptytest.New(t).Attach(inv)
		errC := make(chan error)
		go func() {
			errC <- inv.Run()
		}()
		require.NoError(t, <-errC)
		pty.ExpectMatch(first.OrganizationID.String())
	})

	t.Run("UsingFlag", func(t *testing.T) {
		t.Parallel()
		ownerClient, first := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureMultipleOrganizations:      1,
					wirtualsdk.FeatureExternalProvisionerDaemons: 1,
				},
			},
		})

		// Owner is required to make orgs
		client, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, first.OrganizationID, rbac.RoleOwner())

		ctx := testutil.Context(t, testutil.WaitMedium)
		orgs := map[string]wirtualsdk.Organization{
			"foo": {},
			"bar": {},
		}
		for orgName := range orgs {
			org, err := client.CreateOrganization(ctx, wirtualsdk.CreateOrganizationRequest{
				Name: orgName,
			})
			require.NoError(t, err)
			orgs[orgName] = org
		}

		inv, root := clitest.New(t, "organizations", "show", "selected", "--only-id", "-O=bar")
		clitest.SetupConfig(t, client, root)
		pty := ptytest.New(t).Attach(inv)
		errC := make(chan error)
		go func() {
			errC <- inv.Run()
		}()
		require.NoError(t, <-errC)
		pty.ExpectMatch(orgs["bar"].ID.String())
	})
}
