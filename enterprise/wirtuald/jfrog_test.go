package wirtuald_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbfake"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/rbac"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestJFrogXrayScan(t *testing.T) {
	t.Parallel()

	t.Run("Post/Get", func(t *testing.T) {
		t.Parallel()
		ownerClient, db, owner := wirtualdenttest.NewWithDatabase(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{wirtualsdk.FeatureMultipleExternalAuth: 1},
			},
		})

		tac, ta := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID, rbac.RoleTemplateAdmin())

		wsResp := dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
			OrganizationID: owner.OrganizationID,
			OwnerID:        ta.ID,
		}).WithAgent().Do()

		ws := wirtualdtest.MustWorkspace(t, tac, wsResp.Workspace.ID)
		require.Len(t, ws.LatestBuild.Resources, 1)
		require.Len(t, ws.LatestBuild.Resources[0].Agents, 1)

		agentID := ws.LatestBuild.Resources[0].Agents[0].ID
		expectedPayload := wirtualsdk.JFrogXrayScan{
			WorkspaceID: ws.ID,
			AgentID:     agentID,
			Critical:    19,
			High:        5,
			Medium:      3,
			ResultsURL:  "https://hello-world",
		}

		ctx := testutil.Context(t, testutil.WaitMedium)
		err := tac.PostJFrogXrayScan(ctx, expectedPayload)
		require.NoError(t, err)

		resp1, err := tac.JFrogXRayScan(ctx, ws.ID, agentID)
		require.NoError(t, err)
		require.Equal(t, expectedPayload, resp1)

		// Can update again without error.
		expectedPayload = wirtualsdk.JFrogXrayScan{
			WorkspaceID: ws.ID,
			AgentID:     agentID,
			Critical:    20,
			High:        22,
			Medium:      8,
			ResultsURL:  "https://goodbye-world",
		}
		err = tac.PostJFrogXrayScan(ctx, expectedPayload)
		require.NoError(t, err)

		resp2, err := tac.JFrogXRayScan(ctx, ws.ID, agentID)
		require.NoError(t, err)
		require.NotEqual(t, expectedPayload, resp1)
		require.Equal(t, expectedPayload, resp2)
	})

	t.Run("MemberPostUnauthorized", func(t *testing.T) {
		t.Parallel()

		ownerClient, db, owner := wirtualdenttest.NewWithDatabase(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{wirtualsdk.FeatureMultipleExternalAuth: 1},
			},
		})

		memberClient, member := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID)

		wsResp := dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
			OrganizationID: owner.OrganizationID,
			OwnerID:        member.ID,
		}).WithAgent().Do()

		ws := wirtualdtest.MustWorkspace(t, memberClient, wsResp.Workspace.ID)
		require.Len(t, ws.LatestBuild.Resources, 1)
		require.Len(t, ws.LatestBuild.Resources[0].Agents, 1)

		agentID := ws.LatestBuild.Resources[0].Agents[0].ID
		expectedPayload := wirtualsdk.JFrogXrayScan{
			WorkspaceID: ws.ID,
			AgentID:     agentID,
			Critical:    19,
			High:        5,
			Medium:      3,
			ResultsURL:  "https://hello-world",
		}

		ctx := testutil.Context(t, testutil.WaitMedium)
		err := memberClient.PostJFrogXrayScan(ctx, expectedPayload)
		require.Error(t, err)
		cerr, ok := wirtualsdk.AsError(err)
		require.True(t, ok)
		require.Equal(t, http.StatusNotFound, cerr.StatusCode())

		err = ownerClient.PostJFrogXrayScan(ctx, expectedPayload)
		require.NoError(t, err)

		// We should still be able to fetch.
		resp1, err := memberClient.JFrogXRayScan(ctx, ws.ID, agentID)
		require.NoError(t, err)
		require.Equal(t, expectedPayload, resp1)
	})
}
