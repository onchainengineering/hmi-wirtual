package cli_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbfake"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/util/ptr"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestTemplateEdit(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		ownerClient, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAccessControl: 1,
				},
			},
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
			},
		})

		templateAdmin, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID, rbac.RoleTemplateAdmin())
		version := wirtualdtest.CreateTemplateVersion(t, templateAdmin, owner.OrganizationID, nil)
		_ = wirtualdtest.AwaitTemplateVersionJobCompleted(t, templateAdmin, version.ID)
		template := wirtualdtest.CreateTemplate(t, templateAdmin, owner.OrganizationID, version.ID)
		require.False(t, template.RequireActiveVersion)

		inv, conf := newCLI(t, "templates",
			"edit", template.Name,
			"--require-active-version",
			"-y",
		)

		clitest.SetupConfig(t, templateAdmin, conf)

		err := inv.Run()
		require.NoError(t, err)

		ctx := testutil.Context(t, testutil.WaitMedium)
		template, err = templateAdmin.Template(ctx, template.ID)
		require.NoError(t, err)
		require.True(t, template.RequireActiveVersion)
	})

	t.Run("NotEntitled", func(t *testing.T) {
		t.Parallel()

		client, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{},
			},
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
			},
		})
		templateAdmin, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID, rbac.RoleTemplateAdmin())

		version := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
		_ = wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID)
		require.False(t, template.RequireActiveVersion)

		inv, conf := newCLI(t, "templates",
			"edit", template.Name,
			"--require-active-version",
			"-y",
		)

		clitest.SetupConfig(t, templateAdmin, conf)

		err := inv.Run()
		require.Error(t, err)
		require.Contains(t, err.Error(), "your license is not entitled to use enterprise access control, so you cannot set --require-active-version")
	})

	t.Run("WorkspaceCleanup", func(t *testing.T) {
		t.Parallel()

		ownerClient, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAdvancedTemplateScheduling: 1,
				},
			},
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
			},
		})

		templateAdmin, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID, rbac.RoleTemplateAdmin())
		version := wirtualdtest.CreateTemplateVersion(t, templateAdmin, owner.OrganizationID, nil)
		_ = wirtualdtest.AwaitTemplateVersionJobCompleted(t, templateAdmin, version.ID)
		template := wirtualdtest.CreateTemplate(t, templateAdmin, owner.OrganizationID, version.ID)
		require.False(t, template.RequireActiveVersion)
		const (
			expectedFailureTTL           = time.Hour * 3
			expectedDormancyThreshold    = time.Hour * 4
			expectedDormancyAutoDeletion = time.Minute * 10
		)
		inv, conf := newCLI(t, "templates",
			"edit", template.Name,
			"--failure-ttl="+expectedFailureTTL.String(),
			"--dormancy-threshold="+expectedDormancyThreshold.String(),
			"--dormancy-auto-deletion="+expectedDormancyAutoDeletion.String(),
			"-y",
		)

		clitest.SetupConfig(t, templateAdmin, conf)

		err := inv.Run()
		require.NoError(t, err)

		ctx := testutil.Context(t, testutil.WaitMedium)
		template, err = templateAdmin.Template(ctx, template.ID)
		require.NoError(t, err)
		require.Equal(t, expectedFailureTTL.Milliseconds(), template.FailureTTLMillis)
		require.Equal(t, expectedDormancyThreshold.Milliseconds(), template.TimeTilDormantMillis)
		require.Equal(t, expectedDormancyAutoDeletion.Milliseconds(), template.TimeTilDormantAutoDeleteMillis)

		inv, conf = newCLI(t, "templates",
			"edit", template.Name,
			"--display-name=idc",
			"-y",
		)

		clitest.SetupConfig(t, templateAdmin, conf)

		err = inv.Run()
		require.NoError(t, err)

		// Refetch the template to assert we haven't inadvertently updated
		// the values to their default values.
		template, err = templateAdmin.Template(ctx, template.ID)
		require.NoError(t, err)
		require.Equal(t, expectedFailureTTL.Milliseconds(), template.FailureTTLMillis)
		require.Equal(t, expectedDormancyThreshold.Milliseconds(), template.TimeTilDormantMillis)
		require.Equal(t, expectedDormancyAutoDeletion.Milliseconds(), template.TimeTilDormantAutoDeleteMillis)
	})

	// Test that omitting a flag does not update a template with the
	// default for a flag.
	t.Run("DefaultsDontOverride", func(t *testing.T) {
		t.Parallel()

		ctx := testutil.Context(t, testutil.WaitMedium)
		ownerClient, db, owner := wirtualdenttest.NewWithDatabase(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureAdvancedTemplateScheduling: 1,
					wirtualsdk.FeatureAccessControl:              1,
					wirtualsdk.FeatureTemplateRBAC:               1,
				},
			},
		})

		dbtemplate := dbfake.TemplateVersion(t, db).Seed(database.TemplateVersion{
			CreatedBy:      owner.UserID,
			OrganizationID: owner.OrganizationID,
		}).Do().Template

		var (
			expectedName                 = "template"
			expectedDisplayName          = "template_display"
			expectedDescription          = "My description"
			expectedIcon                 = "icon.pjg"
			expectedDefaultTTLMillis     = time.Hour.Milliseconds()
			expectedAllowAutostart       = false
			expectedAllowAutostop        = false
			expectedFailureTTLMillis     = time.Minute.Milliseconds()
			expectedDormancyMillis       = 2 * time.Minute.Milliseconds()
			expectedAutoDeleteMillis     = 3 * time.Minute.Milliseconds()
			expectedRequireActiveVersion = true
			expectedAllowCancelJobs      = false
			deprecationMessage           = "Deprecate me"
			expectedDisableEveryone      = true
			expectedAutostartDaysOfWeek  = []string{}
			expectedAutoStopDaysOfWeek   = []string{}
			expectedAutoStopWeeks        = 1
		)

		assertFieldsFn := func(t *testing.T, tpl wirtualsdk.Template, acl wirtualsdk.TemplateACL) {
			t.Helper()

			assert.Equal(t, expectedName, tpl.Name)
			assert.Equal(t, expectedDisplayName, tpl.DisplayName)
			assert.Equal(t, expectedDescription, tpl.Description)
			assert.Equal(t, expectedIcon, tpl.Icon)
			assert.Equal(t, expectedDefaultTTLMillis, tpl.DefaultTTLMillis)
			assert.Equal(t, expectedAllowAutostart, tpl.AllowUserAutostart)
			assert.Equal(t, expectedAllowAutostop, tpl.AllowUserAutostop)
			assert.Equal(t, expectedFailureTTLMillis, tpl.FailureTTLMillis)
			assert.Equal(t, expectedDormancyMillis, tpl.TimeTilDormantMillis)
			assert.Equal(t, expectedAutoDeleteMillis, tpl.TimeTilDormantAutoDeleteMillis)
			assert.Equal(t, expectedRequireActiveVersion, tpl.RequireActiveVersion)
			assert.Equal(t, deprecationMessage, tpl.DeprecationMessage)
			assert.Equal(t, expectedAllowCancelJobs, tpl.AllowUserCancelWorkspaceJobs)
			assert.Equal(t, len(acl.Groups) == 0, expectedDisableEveryone)
			assert.Equal(t, expectedAutostartDaysOfWeek, tpl.AutostartRequirement.DaysOfWeek)
			assert.Equal(t, expectedAutoStopDaysOfWeek, tpl.AutostopRequirement.DaysOfWeek)
			assert.Equal(t, int64(expectedAutoStopWeeks), tpl.AutostopRequirement.Weeks)
		}

		template, err := ownerClient.UpdateTemplateMeta(ctx, dbtemplate.ID, wirtualsdk.UpdateTemplateMeta{
			Name:                           expectedName,
			DisplayName:                    expectedDisplayName,
			Description:                    expectedDescription,
			Icon:                           expectedIcon,
			DefaultTTLMillis:               expectedDefaultTTLMillis,
			AllowUserAutostop:              expectedAllowAutostop,
			AllowUserAutostart:             expectedAllowAutostart,
			FailureTTLMillis:               expectedFailureTTLMillis,
			TimeTilDormantMillis:           expectedDormancyMillis,
			TimeTilDormantAutoDeleteMillis: expectedAutoDeleteMillis,
			RequireActiveVersion:           expectedRequireActiveVersion,
			DeprecationMessage:             ptr.Ref(deprecationMessage),
			DisableEveryoneGroupAccess:     expectedDisableEveryone,
			AllowUserCancelWorkspaceJobs:   expectedAllowCancelJobs,
			AutostartRequirement: &wirtualsdk.TemplateAutostartRequirement{
				DaysOfWeek: expectedAutostartDaysOfWeek,
			},
		})
		require.NoError(t, err)

		templateACL, err := ownerClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)

		assertFieldsFn(t, template, templateACL)

		expectedName = "newName"
		inv, conf := newCLI(t, "templates",
			"edit", template.Name,
			"--name=newName",
			"-y",
		)

		clitest.SetupConfig(t, ownerClient, conf)

		err = inv.Run()
		require.NoError(t, err)

		template, err = ownerClient.Template(ctx, template.ID)
		require.NoError(t, err)
		templateACL, err = ownerClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)
		assertFieldsFn(t, template, templateACL)

		expectedAutostartDaysOfWeek = []string{"monday", "wednesday", "friday"}
		expectedAutoStopDaysOfWeek = []string{"tuesday", "thursday"}
		expectedAutoStopWeeks = 2

		template, err = ownerClient.UpdateTemplateMeta(ctx, dbtemplate.ID, wirtualsdk.UpdateTemplateMeta{
			Name:                           expectedName,
			DisplayName:                    expectedDisplayName,
			Description:                    expectedDescription,
			Icon:                           expectedIcon,
			DefaultTTLMillis:               expectedDefaultTTLMillis,
			AllowUserAutostop:              expectedAllowAutostop,
			AllowUserAutostart:             expectedAllowAutostart,
			FailureTTLMillis:               expectedFailureTTLMillis,
			TimeTilDormantMillis:           expectedDormancyMillis,
			TimeTilDormantAutoDeleteMillis: expectedAutoDeleteMillis,
			RequireActiveVersion:           expectedRequireActiveVersion,
			DeprecationMessage:             ptr.Ref(deprecationMessage),
			DisableEveryoneGroupAccess:     expectedDisableEveryone,
			AllowUserCancelWorkspaceJobs:   expectedAllowCancelJobs,
			AutostartRequirement: &wirtualsdk.TemplateAutostartRequirement{
				DaysOfWeek: expectedAutostartDaysOfWeek,
			},

			AutostopRequirement: &wirtualsdk.TemplateAutostopRequirement{
				DaysOfWeek: expectedAutoStopDaysOfWeek,
				Weeks:      int64(expectedAutoStopWeeks),
			},
		})
		require.NoError(t, err)
		assertFieldsFn(t, template, templateACL)

		// Rerun the update so we can assert that autostop days aren't
		// mucked with.
		expectedName = "newName2"
		inv, conf = newCLI(t, "templates",
			"edit", template.Name,
			"--name=newName2",
			"-y",
		)

		clitest.SetupConfig(t, ownerClient, conf)

		err = inv.Run()
		require.NoError(t, err)

		template, err = ownerClient.Template(ctx, template.ID)
		require.NoError(t, err)

		templateACL, err = ownerClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)
		assertFieldsFn(t, template, templateACL)
	})
}
