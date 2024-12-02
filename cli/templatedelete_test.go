package cli_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/pretty"

	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/cli/cliui"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
	"github.com/onchainengineering/hmi-wirtual/pty/ptytest"
)

func TestTemplateDelete(t *testing.T) {
	t.Parallel()

	t.Run("Ok", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
		owner := wirtualdtest.CreateFirstUser(t, client)
		templateAdmin, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID, rbac.RoleTemplateAdmin())
		version := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
		_ = wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID)

		inv, root := clitest.New(t, "templates", "delete", template.Name)

		clitest.SetupConfig(t, templateAdmin, root)
		pty := ptytest.New(t).Attach(inv)

		execDone := make(chan error)
		go func() {
			execDone <- inv.Run()
		}()

		pty.ExpectMatch(fmt.Sprintf("Delete these templates: %s?", pretty.Sprint(cliui.DefaultStyles.Code, template.Name)))
		pty.WriteLine("yes")

		require.NoError(t, <-execDone)

		_, err := client.Template(context.Background(), template.ID)
		require.Error(t, err, "template should not exist")
	})

	t.Run("Multiple --yes", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
		owner := wirtualdtest.CreateFirstUser(t, client)
		templateAdmin, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID, rbac.RoleTemplateAdmin())
		templates := []wirtualsdk.Template{}
		templateNames := []string{}
		for i := 0; i < 3; i++ {
			version := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
			_ = wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
			template := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID)
			templates = append(templates, template)
			templateNames = append(templateNames, template.Name)
		}

		inv, root := clitest.New(t, append([]string{"templates", "delete", "--yes"}, templateNames...)...)
		clitest.SetupConfig(t, templateAdmin, root)
		require.NoError(t, inv.Run())

		for _, template := range templates {
			_, err := client.Template(context.Background(), template.ID)
			require.Error(t, err, "template should not exist")
		}
	})

	t.Run("Multiple prompted", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
		owner := wirtualdtest.CreateFirstUser(t, client)
		templateAdmin, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID, rbac.RoleTemplateAdmin())
		templates := []wirtualsdk.Template{}
		templateNames := []string{}
		for i := 0; i < 3; i++ {
			version := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
			_ = wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
			template := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID)
			templates = append(templates, template)
			templateNames = append(templateNames, template.Name)
		}

		inv, root := clitest.New(t, append([]string{"templates", "delete"}, templateNames...)...)
		clitest.SetupConfig(t, templateAdmin, root)
		pty := ptytest.New(t).Attach(inv)

		execDone := make(chan error)
		go func() {
			execDone <- inv.Run()
		}()

		pty.ExpectMatch(fmt.Sprintf("Delete these templates: %s?", pretty.Sprint(cliui.DefaultStyles.Code, strings.Join(templateNames, ", "))))
		pty.WriteLine("yes")

		require.NoError(t, <-execDone)

		for _, template := range templates {
			_, err := client.Template(context.Background(), template.ID)
			require.Error(t, err, "template should not exist")
		}
	})

	t.Run("Selector", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
		owner := wirtualdtest.CreateFirstUser(t, client)
		templateAdmin, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID, rbac.RoleTemplateAdmin())
		version := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
		_ = wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID)

		inv, root := clitest.New(t, "templates", "delete")
		clitest.SetupConfig(t, templateAdmin, root)

		pty := ptytest.New(t).Attach(inv)

		execDone := make(chan error)
		go func() {
			execDone <- inv.Run()
		}()

		pty.WriteLine("yes")
		require.NoError(t, <-execDone)

		_, err := client.Template(context.Background(), template.ID)
		require.Error(t, err, "template should not exist")
	})
}
