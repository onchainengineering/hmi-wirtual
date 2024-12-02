package wirtuald_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/wirtuald/audit"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/gitsshkey"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk/agentsdk"
	"github.com/onchainengineering/hmi-wirtual/provisioner/echo"
	"github.com/onchainengineering/hmi-wirtual/testutil"
)

func TestGitSSHKey(t *testing.T) {
	t.Parallel()
	t.Run("None", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, nil)
		res := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		key, err := client.GitSSHKey(ctx, res.UserID.String())
		require.NoError(t, err)
		require.NotEmpty(t, key.PublicKey)
	})
	t.Run("Ed25519", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			SSHKeygenAlgorithm: gitsshkey.AlgorithmEd25519,
		})
		res := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		key, err := client.GitSSHKey(ctx, res.UserID.String())
		require.NoError(t, err)
		require.NotEmpty(t, key.PublicKey)
	})
	t.Run("ECDSA", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			SSHKeygenAlgorithm: gitsshkey.AlgorithmECDSA,
		})
		res := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		key, err := client.GitSSHKey(ctx, res.UserID.String())
		require.NoError(t, err)
		require.NotEmpty(t, key.PublicKey)
	})
	t.Run("RSA4096", func(t *testing.T) {
		t.Parallel()
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			SSHKeygenAlgorithm: gitsshkey.AlgorithmRSA4096,
		})
		res := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		key, err := client.GitSSHKey(ctx, res.UserID.String())
		require.NoError(t, err)
		require.NotEmpty(t, key.PublicKey)
	})
	t.Run("Regenerate", func(t *testing.T) {
		t.Parallel()
		auditor := audit.NewMock()
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			SSHKeygenAlgorithm: gitsshkey.AlgorithmEd25519,
			Auditor:            auditor,
		})
		res := wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		key1, err := client.GitSSHKey(ctx, res.UserID.String())
		require.NoError(t, err)
		require.NotEmpty(t, key1.PublicKey)
		key2, err := client.RegenerateGitSSHKey(ctx, res.UserID.String())
		require.NoError(t, err)
		require.GreaterOrEqual(t, key2.UpdatedAt, key1.UpdatedAt)
		require.NotEmpty(t, key2.PublicKey)

		require.Len(t, auditor.AuditLogs(), 2)
		assert.Equal(t, database.AuditActionWrite, auditor.AuditLogs()[1].Action)
	})
}

func TestAgentGitSSHKey(t *testing.T) {
	t.Parallel()

	client := wirtualdtest.New(t, &wirtualdtest.Options{
		IncludeProvisionerDaemon: true,
	})
	user := wirtualdtest.CreateFirstUser(t, client)
	authToken := uuid.NewString()
	version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
		Parse:          echo.ParseComplete,
		ProvisionPlan:  echo.PlanComplete,
		ProvisionApply: echo.ProvisionApplyWithAgent(authToken),
	})
	project := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
	wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
	workspace := wirtualdtest.CreateWorkspace(t, client, project.ID)
	wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)

	agentClient := agentsdk.New(client.URL)
	agentClient.SetSessionToken(authToken)

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()

	agentKey, err := agentClient.GitSSHKey(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, agentKey.PrivateKey)
}
