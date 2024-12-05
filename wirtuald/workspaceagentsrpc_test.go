package wirtuald_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentproto "github.com/onchainengineering/hmi-wirtual/agent/proto"
	"github.com/onchainengineering/hmi-wirtual/provisionersdk/proto"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbfake"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbtime"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk/agentsdk"
)

// Ported to RPC API from wirtuald/workspaceagents_test.go
func TestWorkspaceAgentReportStats(t *testing.T) {
	t.Parallel()

	tickCh := make(chan time.Time)
	flushCh := make(chan int, 1)
	client, db := wirtualdtest.NewWithDatabase(t, &wirtualdtest.Options{
		WorkspaceUsageTrackerFlush: flushCh,
		WorkspaceUsageTrackerTick:  tickCh,
	})
	user := wirtualdtest.CreateFirstUser(t, client)
	r := dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
		OrganizationID: user.OrganizationID,
		OwnerID:        user.UserID,
	}).WithAgent().Do()

	ac := agentsdk.New(client.URL)
	ac.SetSessionToken(r.AgentToken)
	conn, err := ac.ConnectRPC(context.Background())
	require.NoError(t, err)
	defer func() {
		_ = conn.Close()
	}()
	agentAPI := agentproto.NewDRPCAgentClient(conn)

	_, err = agentAPI.UpdateStats(context.Background(), &agentproto.UpdateStatsRequest{
		Stats: &agentproto.Stats{
			ConnectionsByProto:          map[string]int64{"TCP": 1},
			ConnectionCount:             1,
			RxPackets:                   1,
			RxBytes:                     1,
			TxPackets:                   1,
			TxBytes:                     1,
			SessionCountVscode:          1,
			SessionCountJetbrains:       0,
			SessionCountReconnectingPty: 0,
			SessionCountSsh:             0,
			ConnectionMedianLatencyMs:   10,
		},
	})
	require.NoError(t, err)

	tickCh <- dbtime.Now()
	count := <-flushCh
	require.Equal(t, 1, count, "expected one flush with one id")

	newWorkspace, err := client.Workspace(context.Background(), r.Workspace.ID)
	require.NoError(t, err)

	assert.True(t,
		newWorkspace.LastUsedAt.After(r.Workspace.LastUsedAt),
		"%s is not after %s", newWorkspace.LastUsedAt, r.Workspace.LastUsedAt,
	)
}

func TestAgentAPI_LargeManifest(t *testing.T) {
	t.Parallel()
	ctx := testutil.Context(t, testutil.WaitLong)
	client, store := wirtualdtest.NewWithDatabase(t, nil)
	adminUser := wirtualdtest.CreateFirstUser(t, client)
	n := 512000
	longScript := make([]byte, n)
	for i := range longScript {
		longScript[i] = 'q'
	}
	r := dbfake.WorkspaceBuild(t, store, database.WorkspaceTable{
		OrganizationID: adminUser.OrganizationID,
		OwnerID:        adminUser.UserID,
	}).WithAgent(func(agents []*proto.Agent) []*proto.Agent {
		agents[0].Scripts = []*proto.Script{
			{
				Script: string(longScript),
			},
		}
		return agents
	}).Do()
	ac := agentsdk.New(client.URL)
	ac.SetSessionToken(r.AgentToken)
	conn, err := ac.ConnectRPC(ctx)
	defer func() {
		_ = conn.Close()
	}()
	require.NoError(t, err)
	agentAPI := agentproto.NewDRPCAgentClient(conn)
	manifest, err := agentAPI.GetManifest(ctx, &agentproto.GetManifestRequest{})
	require.NoError(t, err)
	require.Len(t, manifest.Scripts, 1)
	require.Len(t, manifest.Scripts[0].Script, n)
}
