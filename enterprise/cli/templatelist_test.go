package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestEnterpriseListTemplates(t *testing.T) {
	t.Parallel()

	t.Run("MultiOrg", func(t *testing.T) {
		t.Parallel()

		client, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
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

		// Template in the first organization
		firstVersion := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
		_ = wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, firstVersion.ID)
		_ = wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, firstVersion.ID)

		secondOrg := wirtualdenttest.CreateOrganization(t, client, wirtualdenttest.CreateOrganizationOptions{
			IncludeProvisionerDaemon: true,
		})
		secondVersion := wirtualdtest.CreateTemplateVersion(t, client, secondOrg.ID, nil)
		_ = wirtualdtest.CreateTemplate(t, client, secondOrg.ID, secondVersion.ID)

		// Create a site wide template admin
		templateAdmin, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID, rbac.RoleTemplateAdmin())

		inv, root := clitest.New(t, "templates", "list", "--output=json")
		clitest.SetupConfig(t, templateAdmin, root)

		ctx, cancelFunc := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancelFunc()

		out := bytes.NewBuffer(nil)
		inv.Stdout = out
		err := inv.WithContext(ctx).Run()
		require.NoError(t, err)

		var templates []wirtualsdk.Template
		require.NoError(t, json.Unmarshal(out.Bytes(), &templates))
		require.Len(t, templates, 2)
	})
}
