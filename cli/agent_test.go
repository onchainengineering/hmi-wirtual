package cli_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/agent"
	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/provisionersdk/proto"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbfake"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk/workspacesdk"
)

func TestWorkspaceAgent(t *testing.T) {
	t.Parallel()

	t.Run("LogDirectory", func(t *testing.T) {
		t.Parallel()

		client, db := wirtualdtest.NewWithDatabase(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		r := dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
			OrganizationID: user.OrganizationID,
			OwnerID:        user.UserID,
		}).
			WithAgent().
			Do()
		logDir := t.TempDir()
		inv, _ := clitest.New(t,
			"agent",
			"--auth", "token",
			"--agent-token", r.AgentToken,
			"--agent-url", client.URL.String(),
			"--log-dir", logDir,
		)

		clitest.Start(t, inv)

		wirtualdtest.AwaitWorkspaceAgents(t, client, r.Workspace.ID)

		require.Eventually(t, func() bool {
			info, err := os.Stat(filepath.Join(logDir, "coder-agent.log"))
			if err != nil {
				return false
			}
			return info.Size() > 0
		}, testutil.WaitLong, testutil.IntervalMedium)
	})

	t.Run("Azure", func(t *testing.T) {
		t.Parallel()
		instanceID := "instanceidentifier"
		certificates, metadataClient := wirtualdtest.NewAzureInstanceIdentity(t, instanceID)
		client, db := wirtualdtest.NewWithDatabase(t, &wirtualdtest.Options{
			AzureCertificates: certificates,
		})
		user := wirtualdtest.CreateFirstUser(t, client)
		r := dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
			OrganizationID: user.OrganizationID,
			OwnerID:        user.UserID,
		}).WithAgent(func(agents []*proto.Agent) []*proto.Agent {
			agents[0].Auth = &proto.Agent_InstanceId{InstanceId: instanceID}
			return agents
		}).Do()

		inv, _ := clitest.New(t, "agent", "--auth", "azure-instance-identity", "--agent-url", client.URL.String())
		inv = inv.WithContext(
			//nolint:revive,staticcheck
			context.WithValue(inv.Context(), "azure-client", metadataClient),
		)

		ctx := inv.Context()
		clitest.Start(t, inv)
		wirtualdtest.NewWorkspaceAgentWaiter(t, client, r.Workspace.ID).
			MatchResources(matchAgentWithVersion).Wait()
		workspace, err := client.Workspace(ctx, r.Workspace.ID)
		require.NoError(t, err)
		resources := workspace.LatestBuild.Resources
		if assert.NotEmpty(t, workspace.LatestBuild.Resources) && assert.NotEmpty(t, resources[0].Agents) {
			assert.NotEmpty(t, resources[0].Agents[0].Version)
		}
		dialer, err := workspacesdk.New(client).
			DialAgent(ctx, resources[0].Agents[0].ID, nil)
		require.NoError(t, err)
		defer dialer.Close()
		require.True(t, dialer.AwaitReachable(ctx))
	})

	t.Run("AWS", func(t *testing.T) {
		t.Parallel()
		instanceID := "instanceidentifier"
		certificates, metadataClient := wirtualdtest.NewAWSInstanceIdentity(t, instanceID)
		client, db := wirtualdtest.NewWithDatabase(t, &wirtualdtest.Options{
			AWSCertificates: certificates,
		})
		user := wirtualdtest.CreateFirstUser(t, client)
		r := dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
			OrganizationID: user.OrganizationID,
			OwnerID:        user.UserID,
		}).WithAgent(func(agents []*proto.Agent) []*proto.Agent {
			agents[0].Auth = &proto.Agent_InstanceId{InstanceId: instanceID}
			return agents
		}).Do()

		inv, _ := clitest.New(t, "agent", "--auth", "aws-instance-identity", "--agent-url", client.URL.String())
		inv = inv.WithContext(
			//nolint:revive,staticcheck
			context.WithValue(inv.Context(), "aws-client", metadataClient),
		)

		clitest.Start(t, inv)
		ctx := inv.Context()
		wirtualdtest.NewWorkspaceAgentWaiter(t, client, r.Workspace.ID).
			MatchResources(matchAgentWithVersion).
			Wait()
		workspace, err := client.Workspace(ctx, r.Workspace.ID)
		require.NoError(t, err)
		resources := workspace.LatestBuild.Resources
		if assert.NotEmpty(t, resources) && assert.NotEmpty(t, resources[0].Agents) {
			assert.NotEmpty(t, resources[0].Agents[0].Version)
		}
		dialer, err := workspacesdk.New(client).
			DialAgent(ctx, resources[0].Agents[0].ID, nil)
		require.NoError(t, err)
		defer dialer.Close()
		require.True(t, dialer.AwaitReachable(ctx))
	})

	t.Run("GoogleCloud", func(t *testing.T) {
		t.Parallel()
		instanceID := "instanceidentifier"
		validator, metadataClient := wirtualdtest.NewGoogleInstanceIdentity(t, instanceID, false)
		client, db := wirtualdtest.NewWithDatabase(t, &wirtualdtest.Options{
			GoogleTokenValidator: validator,
		})
		owner := wirtualdtest.CreateFirstUser(t, client)
		member, memberUser := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID)
		r := dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
			OrganizationID: owner.OrganizationID,
			OwnerID:        memberUser.ID,
		}).WithAgent(func(agents []*proto.Agent) []*proto.Agent {
			agents[0].Auth = &proto.Agent_InstanceId{InstanceId: instanceID}
			return agents
		}).Do()

		inv, cfg := clitest.New(t, "agent", "--auth", "google-instance-identity", "--agent-url", client.URL.String())
		clitest.SetupConfig(t, member, cfg)

		clitest.Start(t,
			inv.WithContext(
				//nolint:revive,staticcheck
				context.WithValue(inv.Context(), "gcp-client", metadataClient),
			),
		)

		ctx := inv.Context()
		wirtualdtest.NewWorkspaceAgentWaiter(t, client, r.Workspace.ID).
			MatchResources(matchAgentWithVersion).
			Wait()
		workspace, err := client.Workspace(ctx, r.Workspace.ID)
		require.NoError(t, err)
		resources := workspace.LatestBuild.Resources
		if assert.NotEmpty(t, resources) && assert.NotEmpty(t, resources[0].Agents) {
			assert.NotEmpty(t, resources[0].Agents[0].Version)
		}
		dialer, err := workspacesdk.New(client).DialAgent(ctx, resources[0].Agents[0].ID, nil)
		require.NoError(t, err)
		defer dialer.Close()
		require.True(t, dialer.AwaitReachable(ctx))
		sshClient, err := dialer.SSHClient(ctx)
		require.NoError(t, err)
		defer sshClient.Close()
		session, err := sshClient.NewSession()
		require.NoError(t, err)
		defer session.Close()
		key := "WIRTUAL_AGENT_TOKEN"
		command := "sh -c 'echo $" + key + "'"
		if runtime.GOOS == "windows" {
			command = "cmd.exe /c echo %" + key + "%"
		}
		token, err := session.CombinedOutput(command)
		require.NoError(t, err)
		_, err = uuid.Parse(strings.TrimSpace(string(token)))
		require.NoError(t, err)
	})

	t.Run("PostStartup", func(t *testing.T) {
		t.Parallel()

		client, db := wirtualdtest.NewWithDatabase(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		r := dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
			OrganizationID: user.OrganizationID,
			OwnerID:        user.UserID,
		}).WithAgent().Do()

		logDir := t.TempDir()
		inv, _ := clitest.New(t,
			"agent",
			"--auth", "token",
			"--agent-token", r.AgentToken,
			"--agent-url", client.URL.String(),
			"--log-dir", logDir,
		)
		// Set the subsystems for the agent.
		inv.Environ.Set(agent.EnvAgentSubsystem, fmt.Sprintf("%s,%s", wirtualsdk.AgentSubsystemExectrace, wirtualsdk.AgentSubsystemEnvbox))

		clitest.Start(t, inv)

		resources := wirtualdtest.NewWorkspaceAgentWaiter(t, client, r.Workspace.ID).
			MatchResources(matchAgentWithSubsystems).Wait()
		require.Len(t, resources, 1)
		require.Len(t, resources[0].Agents, 1)
		require.Len(t, resources[0].Agents[0].Subsystems, 2)
		// Sorted
		require.Equal(t, wirtualsdk.AgentSubsystemEnvbox, resources[0].Agents[0].Subsystems[0])
		require.Equal(t, wirtualsdk.AgentSubsystemExectrace, resources[0].Agents[0].Subsystems[1])
	})
	t.Run("Headers&DERPHeaders", func(t *testing.T) {
		t.Parallel()

		// Create a wirtuald API instance the hard way since we need to change the
		// handler to inject our custom /derp handler.
		dv := wirtualdtest.DeploymentValues(t)
		dv.DERP.Config.BlockDirect = true
		setHandler, cancelFunc, serverURL, newOptions := wirtualdtest.NewOptions(t, &wirtualdtest.Options{
			DeploymentValues: dv,
		})

		// We set the handler after server creation for the access URL.
		coderAPI := wirtuald.New(newOptions)
		setHandler(coderAPI.RootHandler)
		provisionerCloser := wirtualdtest.NewProvisionerDaemon(t, coderAPI)
		t.Cleanup(func() {
			_ = provisionerCloser.Close()
		})
		client := wirtualsdk.New(serverURL)
		t.Cleanup(func() {
			cancelFunc()
			_ = provisionerCloser.Close()
			_ = coderAPI.Close()
			client.HTTPClient.CloseIdleConnections()
		})

		var (
			admin              = wirtualdtest.CreateFirstUser(t, client)
			member, memberUser = wirtualdtest.CreateAnotherUser(t, client, admin.OrganizationID)
			called             int64
			derpCalled         int64
		)

		setHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Ignore client requests
			if r.Header.Get("X-Testing") == "agent" {
				assert.Equal(t, "Ethan was Here!", r.Header.Get("Cool-Header"))
				assert.Equal(t, "very-wow-"+client.URL.String(), r.Header.Get("X-Process-Testing"))
				assert.Equal(t, "more-wow", r.Header.Get("X-Process-Testing2"))
				if strings.HasPrefix(r.URL.Path, "/derp") {
					atomic.AddInt64(&derpCalled, 1)
				} else {
					atomic.AddInt64(&called, 1)
				}
			}
			coderAPI.RootHandler.ServeHTTP(w, r)
		}))
		r := dbfake.WorkspaceBuild(t, coderAPI.Database, database.WorkspaceTable{
			OrganizationID: memberUser.OrganizationIDs[0],
			OwnerID:        memberUser.ID,
		}).WithAgent().Do()

		coderURLEnv := "$WIRTUAL_URL"
		if runtime.GOOS == "windows" {
			coderURLEnv = "%WIRTUAL_URL%"
		}

		logDir := t.TempDir()
		agentInv, _ := clitest.New(t,
			"agent",
			"--auth", "token",
			"--agent-token", r.AgentToken,
			"--agent-url", client.URL.String(),
			"--log-dir", logDir,
			"--agent-header", "X-Testing=agent",
			"--agent-header", "Cool-Header=Ethan was Here!",
			"--agent-header-command", "printf X-Process-Testing=very-wow-"+coderURLEnv+"'\\r\\n'X-Process-Testing2=more-wow",
		)
		clitest.Start(t, agentInv)
		wirtualdtest.NewWorkspaceAgentWaiter(t, client, r.Workspace.ID).
			MatchResources(matchAgentWithVersion).Wait()

		ctx := testutil.Context(t, testutil.WaitLong)
		clientInv, root := clitest.New(t,
			"-v",
			"--no-feature-warning",
			"--no-version-warning",
			"ping", r.Workspace.Name,
			"-n", "1",
		)
		clitest.SetupConfig(t, member, root)
		err := clientInv.WithContext(ctx).Run()
		require.NoError(t, err)

		require.Greater(t, atomic.LoadInt64(&called), int64(0), "expected wirtuald to be reached with custom headers")
		require.Greater(t, atomic.LoadInt64(&derpCalled), int64(0), "expected /derp to be called with custom headers")
	})
}

func matchAgentWithVersion(rs []wirtualsdk.WorkspaceResource) bool {
	if len(rs) < 1 {
		return false
	}
	if len(rs[0].Agents) < 1 {
		return false
	}
	if rs[0].Agents[0].Version == "" {
		return false
	}
	return true
}

func matchAgentWithSubsystems(rs []wirtualsdk.WorkspaceResource) bool {
	if len(rs) < 1 {
		return false
	}
	if len(rs[0].Agents) < 1 {
		return false
	}
	if len(rs[0].Agents[0].Subsystems) < 1 {
		return false
	}
	return true
}
