package wirtualdtest_test

import (
	"testing"

	"go.uber.org/goleak"

	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestNew(t *testing.T) {
	t.Parallel()
	client := wirtualdtest.New(t, &wirtualdtest.Options{
		IncludeProvisionerDaemon: true,
	})
	user := wirtualdtest.CreateFirstUser(t, client)
	version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
	_ = wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
	template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
	workspace := wirtualdtest.CreateWorkspace(t, client, template.ID)
	wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)
	wirtualdtest.AwaitWorkspaceAgents(t, client, workspace.ID)
	_, _ = wirtualdtest.NewGoogleInstanceIdentity(t, "example", false)
	_, _ = wirtualdtest.NewAWSInstanceIdentity(t, "an-instance")
}
