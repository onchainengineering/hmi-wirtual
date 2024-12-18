package exptest_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"cdr.dev/slog/sloggers/slogtest"
	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

// This test validates that the scaletest CLI filters out workspaces not owned
// when disable owner workspace access is set.
// This test is in its own package because it mutates a global variable that
// can influence other tests in the same package.
// nolint:paralleltest
func TestScaleTestWorkspaceTraffic_UseHostLogin(t *testing.T) {
	ctx, cancelFunc := context.WithTimeout(context.Background(), testutil.WaitMedium)
	defer cancelFunc()

	log := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true})
	client := wirtualdtest.New(t, &wirtualdtest.Options{
		Logger:                   &log,
		IncludeProvisionerDaemon: true,
		DeploymentValues: wirtualdtest.DeploymentValues(t, func(dv *wirtualsdk.DeploymentValues) {
			dv.DisableOwnerWorkspaceExec = true
		}),
	})
	owner := wirtualdtest.CreateFirstUser(t, client)
	tv := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
	_ = wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, tv.ID)
	tpl := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, tv.ID)
	// Create a workspace owned by a different user
	memberClient, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID)
	_ = wirtualdtest.CreateWorkspace(t, memberClient, tpl.ID, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
		cwr.Name = "scaletest-workspace"
	})

	// Test without --use-host-login first.g
	inv, root := clitest.New(t, "exp", "scaletest", "workspace-traffic",
		"--template", tpl.Name,
	)
	// nolint:gocritic // We are intentionally testing this as the owner.
	clitest.SetupConfig(t, client, root)
	var stdoutBuf bytes.Buffer
	inv.Stdout = &stdoutBuf

	err := inv.WithContext(ctx).Run()
	require.ErrorContains(t, err, "no scaletest workspaces exist")
	require.Contains(t, stdoutBuf.String(), `1 workspace(s) were skipped`)

	// Test once again with --use-host-login.
	inv, root = clitest.New(t, "exp", "scaletest", "workspace-traffic",
		"--template", tpl.Name,
		"--use-host-login",
	)
	// nolint:gocritic // We are intentionally testing this as the owner.
	clitest.SetupConfig(t, client, root)
	stdoutBuf.Reset()
	inv.Stdout = &stdoutBuf

	err = inv.WithContext(ctx).Run()
	require.ErrorContains(t, err, "no scaletest workspaces exist")
	require.NotContains(t, stdoutBuf.String(), `1 workspace(s) were skipped`)
}
