package cli_test

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/cli/clitest"
	"github.com/coder/coder/v2/wirtuald/coderdtest"
	"github.com/coder/coder/v2/wirtuald/database/dbauthz"
	"github.com/coder/coder/v2/wirtualsdk"
	"github.com/coder/coder/v2/pty/ptytest"
	"github.com/coder/coder/v2/testutil"
)

func TestDelete(t *testing.T) {
	t.Parallel()
	t.Run("WithParameter", func(t *testing.T) {
		t.Parallel()
		client := coderdtest.New(t, &coderdtest.Options{IncludeProvisionerDaemon: true})
		owner := coderdtest.CreateFirstUser(t, client)
		member, _ := coderdtest.CreateAnotherUser(t, client, owner.OrganizationID)
		version := coderdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
		coderdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := coderdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID)
		workspace := coderdtest.CreateWorkspace(t, member, template.ID)
		coderdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)
		inv, root := clitest.New(t, "delete", workspace.Name, "-y")
		clitest.SetupConfig(t, member, root)
		doneChan := make(chan struct{})
		pty := ptytest.New(t).Attach(inv)
		go func() {
			defer close(doneChan)
			err := inv.Run()
			// When running with the race detector on, we sometimes get an EOF.
			if err != nil {
				assert.ErrorIs(t, err, io.EOF)
			}
		}()
		pty.ExpectMatch("has been deleted")
		<-doneChan
	})

	t.Run("Orphan", func(t *testing.T) {
		t.Parallel()
		client := coderdtest.New(t, &coderdtest.Options{IncludeProvisionerDaemon: true})
		owner := coderdtest.CreateFirstUser(t, client)
		version := coderdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
		coderdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := coderdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID)
		workspace := coderdtest.CreateWorkspace(t, client, template.ID)
		coderdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)
		inv, root := clitest.New(t, "delete", workspace.Name, "-y", "--orphan")

		//nolint:gocritic // Deleting orphaned workspaces requires an admin.
		clitest.SetupConfig(t, client, root)
		doneChan := make(chan struct{})
		pty := ptytest.New(t).Attach(inv)
		inv.Stderr = pty.Output()
		go func() {
			defer close(doneChan)
			err := inv.Run()
			// When running with the race detector on, we sometimes get an EOF.
			if err != nil {
				assert.ErrorIs(t, err, io.EOF)
			}
		}()
		pty.ExpectMatch("has been deleted")
		<-doneChan
	})

	// Super orphaned, as the workspace doesn't even have a user.
	// This is not a scenario we should ever get into, as we do not allow users
	// to be deleted if they have workspaces. However issue #7872 shows that
	// it is possible to get into this state. An admin should be able to still
	// force a delete action on the workspace.
	t.Run("OrphanDeletedUser", func(t *testing.T) {
		t.Parallel()
		client, _, api := coderdtest.NewWithAPI(t, &coderdtest.Options{IncludeProvisionerDaemon: true})
		owner := coderdtest.CreateFirstUser(t, client)
		deleteMeClient, deleteMeUser := coderdtest.CreateAnotherUser(t, client, owner.OrganizationID)
		version := coderdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
		coderdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := coderdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID)
		workspace := coderdtest.CreateWorkspace(t, deleteMeClient, template.ID)
		coderdtest.AwaitWorkspaceBuildJobCompleted(t, deleteMeClient, workspace.LatestBuild.ID)

		// The API checks if the user has any workspaces, so we cannot delete a user
		// this way.
		ctx := testutil.Context(t, testutil.WaitShort)
		// nolint:gocritic // Unit test
		err := api.Database.UpdateUserDeletedByID(dbauthz.AsSystemRestricted(ctx), deleteMeUser.ID)
		require.NoError(t, err)

		inv, root := clitest.New(t, "delete", fmt.Sprintf("%s/%s", deleteMeUser.ID, workspace.Name), "-y", "--orphan")

		//nolint:gocritic // Deleting orphaned workspaces requires an admin.
		clitest.SetupConfig(t, client, root)
		doneChan := make(chan struct{})
		pty := ptytest.New(t).Attach(inv)
		inv.Stderr = pty.Output()
		go func() {
			defer close(doneChan)
			err := inv.Run()
			// When running with the race detector on, we sometimes get an EOF.
			if err != nil {
				assert.ErrorIs(t, err, io.EOF)
			}
		}()
		pty.ExpectMatch("has been deleted")
		<-doneChan
	})

	t.Run("DifferentUser", func(t *testing.T) {
		t.Parallel()
		adminClient := coderdtest.New(t, &coderdtest.Options{IncludeProvisionerDaemon: true})
		adminUser := coderdtest.CreateFirstUser(t, adminClient)
		orgID := adminUser.OrganizationID
		client, _ := coderdtest.CreateAnotherUser(t, adminClient, orgID)
		user, err := client.User(context.Background(), wirtualsdk.Me)
		require.NoError(t, err)

		version := coderdtest.CreateTemplateVersion(t, adminClient, orgID, nil)
		coderdtest.AwaitTemplateVersionJobCompleted(t, adminClient, version.ID)
		template := coderdtest.CreateTemplate(t, adminClient, orgID, version.ID)
		workspace := coderdtest.CreateWorkspace(t, client, template.ID)
		coderdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)

		inv, root := clitest.New(t, "delete", user.Username+"/"+workspace.Name, "-y")
		//nolint:gocritic // This requires an admin.
		clitest.SetupConfig(t, adminClient, root)
		doneChan := make(chan struct{})
		pty := ptytest.New(t).Attach(inv)
		go func() {
			defer close(doneChan)
			err := inv.Run()
			// When running with the race detector on, we sometimes get an EOF.
			if err != nil {
				assert.ErrorIs(t, err, io.EOF)
			}
		}()

		pty.ExpectMatch("has been deleted")
		<-doneChan

		workspace, err = client.Workspace(context.Background(), workspace.ID)
		require.ErrorContains(t, err, "was deleted")
	})

	t.Run("InvalidWorkspaceIdentifier", func(t *testing.T) {
		t.Parallel()
		client := coderdtest.New(t, nil)
		inv, root := clitest.New(t, "delete", "a/b/c", "-y")
		clitest.SetupConfig(t, client, root)
		doneChan := make(chan struct{})
		go func() {
			defer close(doneChan)
			err := inv.Run()
			assert.ErrorContains(t, err, "invalid workspace name: \"a/b/c\"")
		}()
		<-doneChan
	})
}
