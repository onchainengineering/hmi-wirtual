package reconnectingpty_test

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/agent/agenttest"
	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
	"github.com/coder/coder/v2/wirtuald/httpapi"
	"github.com/coder/coder/v2/wirtualsdk"
	"github.com/coder/coder/v2/wirtualsdk/workspacesdk"
	"github.com/coder/coder/v2/provisioner/echo"
	"github.com/coder/coder/v2/provisionersdk/proto"
	"github.com/coder/coder/v2/scaletest/reconnectingpty"
	"github.com/coder/coder/v2/testutil"
)

func Test_Runner(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		client, agentID := setupRunnerTest(t)

		runner := reconnectingpty.NewRunner(client, reconnectingpty.Config{
			AgentID: agentID,
			Init: workspacesdk.AgentReconnectingPTYInit{
				// Use ; here because it's powershell compatible (vs &&).
				Command: "echo 'hello world'; sleep 1",
			},
			LogOutput: true,
		})

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitSuperLong)
		defer cancel()

		logs := bytes.NewBuffer(nil)
		err := runner.Run(ctx, "1", logs)
		require.NoError(t, err)

		tr := testutil.NewTerminalReader(t, logs)
		err = tr.ReadUntilString(ctx, "Output:")
		require.NoError(t, err)

		// OSX: Output:\n\thello world\n
		// Win: Output:\n\t\x1b[2J\x1b[m\x1b[H\x1b]0;Administrator: C:\\Program Files\\PowerShell\\7\\pwsh.exe\a\x1b[?25hhello world\n
		err = tr.ReadUntilString(ctx, "hello world")
		require.NoError(t, err)
	})

	t.Run("NoLogOutput", func(t *testing.T) {
		t.Parallel()

		client, agentID := setupRunnerTest(t)

		runner := reconnectingpty.NewRunner(client, reconnectingpty.Config{
			AgentID: agentID,
			Init: workspacesdk.AgentReconnectingPTYInit{
				Command: "echo 'hello world'",
			},
			LogOutput: false,
		})

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitSuperLong)
		defer cancel()

		logs := bytes.NewBuffer(nil)
		err := runner.Run(ctx, "1", logs)
		require.NoError(t, err)

		tr := testutil.NewTerminalReader(t, logs)
		err = tr.ReadUntilString(ctx, "Output:")
		require.Error(t, err)
		require.ErrorIs(t, err, io.EOF)
	})

	t.Run("Timeout", func(t *testing.T) {
		t.Parallel()

		t.Run("NoTimeout", func(t *testing.T) {
			t.Parallel()

			client, agentID := setupRunnerTest(t)

			runner := reconnectingpty.NewRunner(client, reconnectingpty.Config{
				AgentID: agentID,
				Init: workspacesdk.AgentReconnectingPTYInit{
					Command: "echo 'hello world'",
				},
				Timeout:   httpapi.Duration(2 * testutil.WaitSuperLong),
				LogOutput: true,
			})

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitSuperLong)
			defer cancel()

			logs := bytes.NewBuffer(nil)
			err := runner.Run(ctx, "1", logs)
			logStr := logs.String()
			t.Log("Runner logs:\n\n" + logStr)
			require.NoError(t, err)
		})

		t.Run("Timeout", func(t *testing.T) {
			t.Parallel()

			client, agentID := setupRunnerTest(t)

			runner := reconnectingpty.NewRunner(client, reconnectingpty.Config{
				AgentID: agentID,
				Init: workspacesdk.AgentReconnectingPTYInit{
					Command: "sleep 120",
				},
				Timeout:   httpapi.Duration(2 * time.Second),
				LogOutput: true,
			})

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitSuperLong)
			defer cancel()

			logs := bytes.NewBuffer(nil)
			err := runner.Run(ctx, "1", logs)
			logStr := logs.String()
			t.Log("Runner logs:\n\n" + logStr)
			require.Error(t, err)
			require.ErrorIs(t, err, context.DeadlineExceeded)
		})
	})

	t.Run("ExpectTimeout", func(t *testing.T) {
		t.Parallel()

		t.Run("Timeout", func(t *testing.T) {
			t.Parallel()

			client, agentID := setupRunnerTest(t)

			runner := reconnectingpty.NewRunner(client, reconnectingpty.Config{
				AgentID: agentID,
				Init: workspacesdk.AgentReconnectingPTYInit{
					Command: "sleep 120",
				},
				Timeout:       httpapi.Duration(2 * time.Second),
				ExpectTimeout: true,
				LogOutput:     true,
			})

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitSuperLong)
			defer cancel()

			logs := bytes.NewBuffer(nil)
			err := runner.Run(ctx, "1", logs)
			logStr := logs.String()
			t.Log("Runner logs:\n\n" + logStr)
			require.NoError(t, err)
		})

		t.Run("NoTimeout", func(t *testing.T) {
			t.Parallel()

			client, agentID := setupRunnerTest(t)

			runner := reconnectingpty.NewRunner(client, reconnectingpty.Config{
				AgentID: agentID,
				Init: workspacesdk.AgentReconnectingPTYInit{
					Command: "echo 'hello world'",
				},
				Timeout:       httpapi.Duration(2 * testutil.WaitSuperLong),
				ExpectTimeout: true,
				LogOutput:     true,
			})

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitSuperLong)
			defer cancel()

			logs := bytes.NewBuffer(nil)
			err := runner.Run(ctx, "1", logs)
			logStr := logs.String()
			t.Log("Runner logs:\n\n" + logStr)
			require.Error(t, err)
			require.ErrorContains(t, err, "expected timeout")
		})
	})

	t.Run("ExpectOutput", func(t *testing.T) {
		t.Parallel()

		t.Run("Matches", func(t *testing.T) {
			t.Parallel()

			client, agentID := setupRunnerTest(t)

			runner := reconnectingpty.NewRunner(client, reconnectingpty.Config{
				AgentID: agentID,
				Init: workspacesdk.AgentReconnectingPTYInit{
					Command: "echo 'hello world'; sleep 1",
				},
				LogOutput: true,
			})

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitSuperLong)
			defer cancel()

			logs := bytes.NewBuffer(nil)
			err := runner.Run(ctx, "1", logs)
			require.NoError(t, err)

			tr := testutil.NewTerminalReader(t, logs)
			err = tr.ReadUntilString(ctx, "hello world")
			require.NoError(t, err)
		})

		t.Run("NotMatches", func(t *testing.T) {
			t.Parallel()

			client, agentID := setupRunnerTest(t)

			runner := reconnectingpty.NewRunner(client, reconnectingpty.Config{
				AgentID: agentID,
				Init: workspacesdk.AgentReconnectingPTYInit{
					Command: "echo 'hello world'; sleep 1",
				},
				LogOutput: true,
			})

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitSuperLong)
			defer cancel()

			logs := bytes.NewBuffer(nil)
			err := runner.Run(ctx, "1", logs)
			require.NoError(t, err)

			tr := testutil.NewTerminalReader(t, logs)
			err = tr.ReadUntilString(ctx, "bello borld")
			require.Error(t, err)
			require.ErrorIs(t, err, io.EOF)
		})
	})
}

func setupRunnerTest(t *testing.T) (client *wirtualsdk.Client, agentID uuid.UUID) {
	t.Helper()

	client, _, api := wirtualdtest.NewWithAPI(t, &wirtualdtest.Options{
		IncludeProvisionerDaemon: true,
	})
	user := wirtualdtest.CreateFirstUser(t, client)

	authToken := uuid.NewString()
	version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
		Parse:         echo.ParseComplete,
		ProvisionPlan: echo.PlanComplete,
		ProvisionApply: []*proto.Response{{
			Type: &proto.Response_Apply{
				Apply: &proto.ApplyComplete{
					Resources: []*proto.Resource{{
						Name: "example",
						Type: "aws_instance",
						Agents: []*proto.Agent{{
							Id:   uuid.NewString(),
							Name: "agent",
							Auth: &proto.Agent_Token{
								Token: authToken,
							},
							Apps: []*proto.App{},
						}},
					}},
				},
			},
		}},
	})

	template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
	wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)

	workspace := wirtualdtest.CreateWorkspace(t, client, template.ID)
	wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)

	_ = agenttest.New(t, client.URL, authToken)
	resources := wirtualdtest.AwaitWorkspaceAgents(t, client, workspace.ID)
	require.Eventually(t, func() bool {
		t.Log("agent id", resources[0].Agents[0].ID)
		return (*api.TailnetCoordinator.Load()).Node(resources[0].Agents[0].ID) != nil
	}, testutil.WaitLong, testutil.IntervalMedium, "agent never connected")
	return client, resources[0].Agents[0].ID
}
