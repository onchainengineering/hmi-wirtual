package cli_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/agent/agenttest"
	agentproto "github.com/onchainengineering/hmi-wirtual/agent/proto"
	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/pty/ptytest"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbfake"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/workspacestats/workspacestatstest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

// TestVSCodeSSH ensures the agent connects properly with SSH
// and that network information is properly written to the FS.
func TestVSCodeSSH(t *testing.T) {
	t.Parallel()
	ctx := testutil.Context(t, testutil.WaitLong)
	dv := wirtualdtest.DeploymentValues(t)
	dv.Experiments = []string{string(wirtualsdk.ExperimentWorkspaceUsage)}
	batcher := &workspacestatstest.StatsBatcher{
		LastStats: &agentproto.Stats{},
	}
	admin, store := wirtualdtest.NewWithDatabase(t, &wirtualdtest.Options{
		DeploymentValues: dv,
		StatsBatcher:     batcher,
	})
	admin.SetLogger(testutil.Logger(t).Named("client"))
	first := wirtualdtest.CreateFirstUser(t, admin)
	client, user := wirtualdtest.CreateAnotherUser(t, admin, first.OrganizationID)
	r := dbfake.WorkspaceBuild(t, store, database.WorkspaceTable{
		OrganizationID: first.OrganizationID,
		OwnerID:        user.ID,
	}).WithAgent().Do()
	workspace := r.Workspace
	agentToken := r.AgentToken

	user, err := client.User(ctx, wirtualsdk.Me)
	require.NoError(t, err)

	_ = agenttest.New(t, client.URL, agentToken)
	_ = wirtualdtest.AwaitWorkspaceAgents(t, client, workspace.ID)

	fs := afero.NewMemMapFs()
	err = afero.WriteFile(fs, "/url", []byte(client.URL.String()), 0o600)
	require.NoError(t, err)
	err = afero.WriteFile(fs, "/token", []byte(client.SessionToken()), 0o600)
	require.NoError(t, err)

	//nolint:revive,staticcheck
	ctx = context.WithValue(ctx, "fs", fs)

	inv, _ := clitest.New(t,
		"vscodessh",
		"--url-file", "/url",
		"--session-token-file", "/token",
		"--network-info-dir", "/net",
		"--log-dir", "/log",
		"--network-info-interval", "25ms",
		fmt.Sprintf("coder-vscode--%s--%s", user.Username, workspace.Name),
	)
	ptytest.New(t).Attach(inv)

	waiter := clitest.StartWithWaiter(t, inv.WithContext(ctx))

	for _, dir := range []string{"/net", "/log"} {
		assert.Eventually(t, func() bool {
			entries, err := afero.ReadDir(fs, dir)
			if err != nil {
				return false
			}
			return len(entries) > 0
		}, testutil.WaitLong, testutil.IntervalFast)
	}
	waiter.Cancel()

	if err := waiter.Wait(); err != nil {
		waiter.RequireIs(context.Canceled)
	}

	require.EqualValues(t, 1, batcher.Called)
	require.EqualValues(t, 1, batcher.LastStats.SessionCountVscode)
}
