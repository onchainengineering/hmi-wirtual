package wirtuald_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/provisionersdk/proto"
	"github.com/coder/coder/v2/testutil"
	"github.com/coder/coder/v2/wirtuald/database"
	"github.com/coder/coder/v2/wirtuald/database/dbauthz"
	"github.com/coder/coder/v2/wirtuald/database/dbfake"
	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
	"github.com/coder/coder/v2/wirtualsdk"
)

func TestPostWorkspaceAgentPortShare(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()
	ownerClient, db := wirtualdtest.NewWithDatabase(t, nil)
	owner := wirtualdtest.CreateFirstUser(t, ownerClient)
	client, user := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID)

	tmpDir := t.TempDir()
	r := dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
		OrganizationID: owner.OrganizationID,
		OwnerID:        user.ID,
	}).WithAgent(func(agents []*proto.Agent) []*proto.Agent {
		agents[0].Directory = tmpDir
		return agents
	}).Do()
	agents, err := db.GetWorkspaceAgentsInLatestBuildByWorkspaceID(dbauthz.As(ctx, wirtualdtest.AuthzUserSubject(user, owner.OrganizationID)), r.Workspace.ID)
	require.NoError(t, err)

	// owner level should fail
	_, err = client.UpsertWorkspaceAgentPortShare(ctx, r.Workspace.ID, wirtualsdk.UpsertWorkspaceAgentPortShareRequest{
		AgentName:  agents[0].Name,
		Port:       8080,
		ShareLevel: wirtualsdk.WorkspaceAgentPortShareLevel("owner"),
		Protocol:   wirtualsdk.WorkspaceAgentPortShareProtocolHTTP,
	})
	require.Error(t, err)

	// invalid level should fail
	_, err = client.UpsertWorkspaceAgentPortShare(ctx, r.Workspace.ID, wirtualsdk.UpsertWorkspaceAgentPortShareRequest{
		AgentName:  agents[0].Name,
		Port:       8080,
		ShareLevel: wirtualsdk.WorkspaceAgentPortShareLevel("invalid"),
		Protocol:   wirtualsdk.WorkspaceAgentPortShareProtocolHTTP,
	})
	require.Error(t, err)

	// invalid protocol should fail
	_, err = client.UpsertWorkspaceAgentPortShare(ctx, r.Workspace.ID, wirtualsdk.UpsertWorkspaceAgentPortShareRequest{
		AgentName:  agents[0].Name,
		Port:       8080,
		ShareLevel: wirtualsdk.WorkspaceAgentPortShareLevelPublic,
		Protocol:   wirtualsdk.WorkspaceAgentPortShareProtocol("invalid"),
	})
	require.Error(t, err)

	// invalid port should fail
	_, err = client.UpsertWorkspaceAgentPortShare(ctx, r.Workspace.ID, wirtualsdk.UpsertWorkspaceAgentPortShareRequest{
		AgentName:  agents[0].Name,
		Port:       0,
		ShareLevel: wirtualsdk.WorkspaceAgentPortShareLevelPublic,
		Protocol:   wirtualsdk.WorkspaceAgentPortShareProtocolHTTP,
	})
	require.Error(t, err)
	_, err = client.UpsertWorkspaceAgentPortShare(ctx, r.Workspace.ID, wirtualsdk.UpsertWorkspaceAgentPortShareRequest{
		AgentName:  agents[0].Name,
		Port:       90000000,
		ShareLevel: wirtualsdk.WorkspaceAgentPortShareLevelPublic,
	})
	require.Error(t, err)

	// OK, ignoring template max port share level because we are AGPL
	ps, err := client.UpsertWorkspaceAgentPortShare(ctx, r.Workspace.ID, wirtualsdk.UpsertWorkspaceAgentPortShareRequest{
		AgentName:  agents[0].Name,
		Port:       8080,
		ShareLevel: wirtualsdk.WorkspaceAgentPortShareLevelPublic,
		Protocol:   wirtualsdk.WorkspaceAgentPortShareProtocolHTTPS,
	})
	require.NoError(t, err)
	require.EqualValues(t, wirtualsdk.WorkspaceAgentPortShareLevelPublic, ps.ShareLevel)
	require.EqualValues(t, wirtualsdk.WorkspaceAgentPortShareProtocolHTTPS, ps.Protocol)

	// list
	list, err := client.GetWorkspaceAgentPortShares(ctx, r.Workspace.ID)
	require.NoError(t, err)
	require.Len(t, list.Shares, 1)
	require.EqualValues(t, agents[0].Name, list.Shares[0].AgentName)
	require.EqualValues(t, 8080, list.Shares[0].Port)
	require.EqualValues(t, wirtualsdk.WorkspaceAgentPortShareLevelPublic, list.Shares[0].ShareLevel)
	require.EqualValues(t, wirtualsdk.WorkspaceAgentPortShareProtocolHTTPS, list.Shares[0].Protocol)

	// update share level and protocol
	ps, err = client.UpsertWorkspaceAgentPortShare(ctx, r.Workspace.ID, wirtualsdk.UpsertWorkspaceAgentPortShareRequest{
		AgentName:  agents[0].Name,
		Port:       8080,
		ShareLevel: wirtualsdk.WorkspaceAgentPortShareLevelAuthenticated,
		Protocol:   wirtualsdk.WorkspaceAgentPortShareProtocolHTTP,
	})
	require.NoError(t, err)
	require.EqualValues(t, wirtualsdk.WorkspaceAgentPortShareLevelAuthenticated, ps.ShareLevel)
	require.EqualValues(t, wirtualsdk.WorkspaceAgentPortShareProtocolHTTP, ps.Protocol)

	// list
	list, err = client.GetWorkspaceAgentPortShares(ctx, r.Workspace.ID)
	require.NoError(t, err)
	require.Len(t, list.Shares, 1)
	require.EqualValues(t, agents[0].Name, list.Shares[0].AgentName)
	require.EqualValues(t, 8080, list.Shares[0].Port)
	require.EqualValues(t, wirtualsdk.WorkspaceAgentPortShareLevelAuthenticated, list.Shares[0].ShareLevel)
	require.EqualValues(t, wirtualsdk.WorkspaceAgentPortShareProtocolHTTP, list.Shares[0].Protocol)

	// list 2 ordered by port
	ps, err = client.UpsertWorkspaceAgentPortShare(ctx, r.Workspace.ID, wirtualsdk.UpsertWorkspaceAgentPortShareRequest{
		AgentName:  agents[0].Name,
		Port:       8081,
		ShareLevel: wirtualsdk.WorkspaceAgentPortShareLevelPublic,
		Protocol:   wirtualsdk.WorkspaceAgentPortShareProtocolHTTPS,
	})
	require.NoError(t, err)
	list, err = client.GetWorkspaceAgentPortShares(ctx, r.Workspace.ID)
	require.NoError(t, err)
	require.Len(t, list.Shares, 2)
	require.EqualValues(t, 8080, list.Shares[0].Port)
	require.EqualValues(t, 8081, list.Shares[1].Port)
}

func TestGetWorkspaceAgentPortShares(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()

	ownerClient, db := wirtualdtest.NewWithDatabase(t, nil)
	owner := wirtualdtest.CreateFirstUser(t, ownerClient)
	client, user := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID)

	tmpDir := t.TempDir()
	r := dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
		OrganizationID: owner.OrganizationID,
		OwnerID:        user.ID,
	}).WithAgent(func(agents []*proto.Agent) []*proto.Agent {
		agents[0].Directory = tmpDir
		return agents
	}).Do()
	agents, err := db.GetWorkspaceAgentsInLatestBuildByWorkspaceID(dbauthz.As(ctx, wirtualdtest.AuthzUserSubject(user, owner.OrganizationID)), r.Workspace.ID)
	require.NoError(t, err)

	_, err = client.UpsertWorkspaceAgentPortShare(ctx, r.Workspace.ID, wirtualsdk.UpsertWorkspaceAgentPortShareRequest{
		AgentName:  agents[0].Name,
		Port:       8080,
		ShareLevel: wirtualsdk.WorkspaceAgentPortShareLevelPublic,
		Protocol:   wirtualsdk.WorkspaceAgentPortShareProtocolHTTP,
	})
	require.NoError(t, err)

	ps, err := client.GetWorkspaceAgentPortShares(ctx, r.Workspace.ID)
	require.NoError(t, err)
	require.Len(t, ps.Shares, 1)
	require.EqualValues(t, agents[0].Name, ps.Shares[0].AgentName)
	require.EqualValues(t, 8080, ps.Shares[0].Port)
	require.EqualValues(t, wirtualsdk.WorkspaceAgentPortShareLevelPublic, ps.Shares[0].ShareLevel)
}

func TestDeleteWorkspaceAgentPortShare(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()

	ownerClient, db := wirtualdtest.NewWithDatabase(t, nil)
	owner := wirtualdtest.CreateFirstUser(t, ownerClient)
	client, user := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID)

	tmpDir := t.TempDir()
	r := dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
		OrganizationID: owner.OrganizationID,
		OwnerID:        user.ID,
	}).WithAgent(func(agents []*proto.Agent) []*proto.Agent {
		agents[0].Directory = tmpDir
		return agents
	}).Do()
	agents, err := db.GetWorkspaceAgentsInLatestBuildByWorkspaceID(dbauthz.As(ctx, wirtualdtest.AuthzUserSubject(user, owner.OrganizationID)), r.Workspace.ID)
	require.NoError(t, err)

	// create
	ps, err := client.UpsertWorkspaceAgentPortShare(ctx, r.Workspace.ID, wirtualsdk.UpsertWorkspaceAgentPortShareRequest{
		AgentName:  agents[0].Name,
		Port:       8080,
		ShareLevel: wirtualsdk.WorkspaceAgentPortShareLevelPublic,
		Protocol:   wirtualsdk.WorkspaceAgentPortShareProtocolHTTP,
	})
	require.NoError(t, err)
	require.EqualValues(t, wirtualsdk.WorkspaceAgentPortShareLevelPublic, ps.ShareLevel)

	// delete
	err = client.DeleteWorkspaceAgentPortShare(ctx, r.Workspace.ID, wirtualsdk.DeleteWorkspaceAgentPortShareRequest{
		AgentName: agents[0].Name,
		Port:      8080,
	})
	require.NoError(t, err)

	// delete missing
	err = client.DeleteWorkspaceAgentPortShare(ctx, r.Workspace.ID, wirtualsdk.DeleteWorkspaceAgentPortShareRequest{
		AgentName: agents[0].Name,
		Port:      8080,
	})
	require.Error(t, err)

	_, err = db.GetWorkspaceAgentPortShare(dbauthz.As(ctx, wirtualdtest.AuthzUserSubject(user, owner.OrganizationID)), database.GetWorkspaceAgentPortShareParams{
		WorkspaceID: r.Workspace.ID,
		AgentName:   agents[0].Name,
		Port:        8080,
	})
	require.Error(t, err)
}
