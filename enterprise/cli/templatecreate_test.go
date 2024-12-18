package cli_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/provisioner/echo"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestTemplateCreate(t *testing.T) {
	t.Parallel()

	t.Run("RequireActiveVersion", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAccessControl: 1,
				},
			},
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
			},
		})
		templateAdmin, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

		source := clitest.CreateTemplateVersionSource(t, &echo.Responses{
			Parse:          echo.ParseComplete,
			ProvisionApply: echo.ApplyComplete,
		})

		inv, conf := newCLI(t, "templates",
			"create", "new-template",
			"--directory", source,
			"--test.provisioner", string(database.ProvisionerTypeEcho),
			"--require-active-version",
			"-y",
		)

		clitest.SetupConfig(t, templateAdmin, conf)

		err := inv.Run()
		require.NoError(t, err)

		ctx := testutil.Context(t, testutil.WaitMedium)
		template, err := templateAdmin.TemplateByName(ctx, user.OrganizationID, "new-template")
		require.NoError(t, err)
		require.True(t, template.RequireActiveVersion)
	})

	t.Run("WorkspaceCleanup", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAdvancedTemplateScheduling: 1,
				},
			},
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
			},
		})
		templateAdmin, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

		source := clitest.CreateTemplateVersionSource(t, &echo.Responses{
			Parse:          echo.ParseComplete,
			ProvisionApply: echo.ApplyComplete,
		})

		const (
			expectedFailureTTL           = time.Hour * 3
			expectedDormancyThreshold    = time.Hour * 4
			expectedDormancyAutoDeletion = time.Minute * 10
		)

		inv, conf := newCLI(t, "templates",
			"create", "new-template",
			"--directory", source,
			"--test.provisioner", string(database.ProvisionerTypeEcho),
			"--failure-ttl="+expectedFailureTTL.String(),
			"--dormancy-threshold="+expectedDormancyThreshold.String(),
			"--dormancy-auto-deletion="+expectedDormancyAutoDeletion.String(),
			"-y",
			"--",
		)

		clitest.SetupConfig(t, templateAdmin, conf)

		err := inv.Run()
		require.NoError(t, err)

		ctx := testutil.Context(t, testutil.WaitMedium)
		template, err := templateAdmin.TemplateByName(ctx, user.OrganizationID, "new-template")
		require.NoError(t, err)
		require.Equal(t, expectedFailureTTL.Milliseconds(), template.FailureTTLMillis)
		require.Equal(t, expectedDormancyThreshold.Milliseconds(), template.TimeTilDormantMillis)
		require.Equal(t, expectedDormancyAutoDeletion.Milliseconds(), template.TimeTilDormantAutoDeleteMillis)
	})

	t.Run("NotEntitled", func(t *testing.T) {
		t.Parallel()

		client, admin := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{},
			},
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
			},
		})
		templateAdmin, _ := wirtualdtest.CreateAnotherUser(t, client, admin.OrganizationID, rbac.RoleTemplateAdmin())

		inv, conf := newCLI(t, "templates",
			"create", "new-template",
			"--require-active-version",
			"-y",
		)

		clitest.SetupConfig(t, templateAdmin, conf)

		err := inv.Run()
		require.Error(t, err)
		require.Contains(t, err.Error(), "your license is not entitled to use enterprise access control, so you cannot set --require-active-version")
	})

	// Create a template in a second organization via custom role
	t.Run("SecondOrganization", func(t *testing.T) {
		t.Parallel()

		ownerClient, _ := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				// This only affects the first org.
				IncludeProvisionerDaemon: false,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAccessControl:              1,
					wirtualsdk.FeatureCustomRoles:                1,
					wirtualsdk.FeatureExternalProvisionerDaemons: 1,
					wirtualsdk.FeatureMultipleOrganizations:      1,
				},
			},
		})

		// Create the second organization
		secondOrg := wirtualdenttest.CreateOrganization(t, ownerClient, wirtualdenttest.CreateOrganizationOptions{
			IncludeProvisionerDaemon: true,
		})

		ctx := testutil.Context(t, testutil.WaitMedium)

		//nolint:gocritic // owner required to make custom roles
		orgTemplateAdminRole, err := ownerClient.CreateOrganizationRole(ctx, wirtualsdk.Role{
			Name:           "org-template-admin",
			OrganizationID: secondOrg.ID.String(),
			OrganizationPermissions: wirtualsdk.CreatePermissions(map[wirtualsdk.RBACResource][]wirtualsdk.RBACAction{
				wirtualsdk.ResourceTemplate: wirtualsdk.RBACResourceActions[wirtualsdk.ResourceTemplate],
			}),
		})
		require.NoError(t, err, "create admin role")

		orgTemplateAdmin, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, secondOrg.ID, rbac.RoleIdentifier{
			Name:           orgTemplateAdminRole.Name,
			OrganizationID: secondOrg.ID,
		})

		source := clitest.CreateTemplateVersionSource(t, &echo.Responses{
			Parse:          echo.ParseComplete,
			ProvisionApply: echo.ApplyComplete,
		})

		const templateName = "new-template"
		inv, conf := newCLI(t, "templates",
			"push", templateName,
			"--directory", source,
			"--test.provisioner", string(database.ProvisionerTypeEcho),
			"-y",
		)

		clitest.SetupConfig(t, orgTemplateAdmin, conf)

		err = inv.Run()
		require.NoError(t, err)

		ctx = testutil.Context(t, testutil.WaitMedium)
		template, err := orgTemplateAdmin.TemplateByName(ctx, secondOrg.ID, templateName)
		require.NoError(t, err)
		require.Equal(t, template.OrganizationID, secondOrg.ID)
	})
}
