package cli_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/cli/clitest"
	"github.com/coder/coder/v2/pty/ptytest"
	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
	"github.com/coder/coder/v2/wirtualsdk"
)

func TestTemplateVersions(t *testing.T) {
	t.Parallel()
	t.Run("ListVersions", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
		owner := wirtualdtest.CreateFirstUser(t, client)
		member, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
		_ = wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID)

		inv, root := clitest.New(t, "templates", "versions", "list", template.Name)
		clitest.SetupConfig(t, member, root)

		pty := ptytest.New(t).Attach(inv)

		errC := make(chan error)
		go func() {
			errC <- inv.Run()
		}()

		require.NoError(t, <-errC)

		pty.ExpectMatch(version.Name)
		pty.ExpectMatch(version.CreatedBy.Username)
		pty.ExpectMatch("Active")
	})
}

func TestTemplateVersionsPromote(t *testing.T) {
	t.Parallel()

	t.Run("PromoteVersion", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
		owner := wirtualdtest.CreateFirstUser(t, client)

		// Create a template with two versions
		version1 := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, completeWithAgent())
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version1.ID)

		template := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version1.ID)

		version2 := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, completeWithAgent(), func(ctvr *wirtualsdk.CreateTemplateVersionRequest) {
			ctvr.TemplateID = template.ID
			ctvr.Name = "2.0.0"
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version2.ID)

		// Ensure version1 is active
		updatedTemplate, err := client.Template(context.Background(), template.ID)
		assert.NoError(t, err)
		assert.Equal(t, version1.ID, updatedTemplate.ActiveVersionID)

		args := []string{
			"templates",
			"versions",
			"promote",
			"--template", template.Name,
			"--template-version", version2.Name,
		}

		inv, root := clitest.New(t, args...)
		//nolint:gocritic // Creating a workspace for another user requires owner permissions.
		clitest.SetupConfig(t, client, root)
		errC := make(chan error)
		go func() {
			errC <- inv.Run()
		}()

		require.NoError(t, <-errC)

		// Verify that version2 is now the active version
		updatedTemplate, err = client.Template(context.Background(), template.ID)
		require.NoError(t, err)
		assert.Equal(t, version2.ID, updatedTemplate.ActiveVersionID)
	})

	t.Run("PromoteNonExistentVersion", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
		owner := wirtualdtest.CreateFirstUser(t, client)
		member, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID)

		version := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
		_ = wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID)

		inv, root := clitest.New(t, "templates", "versions", "promote", "--template", template.Name, "--template-version", "non-existent-version")
		clitest.SetupConfig(t, member, root)

		err := inv.Run()
		require.Error(t, err)
		require.Contains(t, err.Error(), "get template version by name")
	})

	t.Run("PromoteVersionInvalidTemplate", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
		owner := wirtualdtest.CreateFirstUser(t, client)
		member, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID)

		inv, root := clitest.New(t, "templates", "versions", "promote", "--template", "non-existent-template", "--template-version", "some-version")
		clitest.SetupConfig(t, member, root)

		err := inv.Run()
		require.Error(t, err)
		require.Contains(t, err.Error(), "get template by name")
	})
}
