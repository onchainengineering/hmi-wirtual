package cli_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"

	"github.com/onchainengineering/hmi-wirtual/cli/clitest"
	"github.com/onchainengineering/hmi-wirtual/provisioner/echo"
	"github.com/onchainengineering/hmi-wirtual/provisionersdk/proto"
	"github.com/onchainengineering/hmi-wirtual/pty/ptytest"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbfake"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbtestutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

const (
	ephemeralParameterName        = "ephemeral_parameter"
	ephemeralParameterDescription = "This is ephemeral parameter"
	ephemeralParameterValue       = "3"

	immutableParameterName        = "immutable_parameter"
	immutableParameterDescription = "This is immutable parameter"
	immutableParameterValue       = "abc"

	mutableParameterName  = "mutable_parameter"
	mutableParameterValue = "hello"
)

var (
	mutableParamsResponse = &echo.Responses{
		Parse: echo.ParseComplete,
		ProvisionPlan: []*proto.Response{
			{
				Type: &proto.Response_Plan{
					Plan: &proto.PlanComplete{
						Parameters: []*proto.RichParameter{
							{
								Name:        mutableParameterName,
								Description: "This is a mutable parameter",
								Required:    true,
								Mutable:     true,
							},
						},
					},
				},
			},
		},
		ProvisionApply: echo.ApplyComplete,
	}

	immutableParamsResponse = &echo.Responses{
		Parse: echo.ParseComplete,
		ProvisionPlan: []*proto.Response{
			{
				Type: &proto.Response_Plan{
					Plan: &proto.PlanComplete{
						Parameters: []*proto.RichParameter{
							{
								Name:        immutableParameterName,
								Description: immutableParameterDescription,
								Required:    true,
							},
						},
					},
				},
			},
		},
		ProvisionApply: echo.ApplyComplete,
	}
)

func TestStart(t *testing.T) {
	t.Parallel()

	echoResponses := &echo.Responses{
		Parse: echo.ParseComplete,
		ProvisionPlan: []*proto.Response{
			{
				Type: &proto.Response_Plan{
					Plan: &proto.PlanComplete{
						Parameters: []*proto.RichParameter{
							{
								Name:        ephemeralParameterName,
								Description: ephemeralParameterDescription,
								Mutable:     true,
								Ephemeral:   true,
							},
						},
					},
				},
			},
		},
		ProvisionApply: echo.ApplyComplete,
	}

	t.Run("BuildOptions", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
		owner := wirtualdtest.CreateFirstUser(t, client)
		member, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, echoResponses)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID)
		workspace := wirtualdtest.CreateWorkspace(t, member, template.ID)
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)
		// Stop the workspace
		workspaceBuild := wirtualdtest.CreateWorkspaceBuild(t, client, workspace, database.WorkspaceTransitionStop)
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspaceBuild.ID)

		inv, root := clitest.New(t, "start", workspace.Name, "--prompt-ephemeral-parameters")
		clitest.SetupConfig(t, member, root)
		doneChan := make(chan struct{})
		pty := ptytest.New(t).Attach(inv)
		go func() {
			defer close(doneChan)
			err := inv.Run()
			assert.NoError(t, err)
		}()

		matches := []string{
			ephemeralParameterDescription, ephemeralParameterValue,
			"workspace has been started", "",
		}
		for i := 0; i < len(matches); i += 2 {
			match := matches[i]
			value := matches[i+1]
			pty.ExpectMatch(match)

			if value != "" {
				pty.WriteLine(value)
			}
		}
		<-doneChan

		// Verify if ephemeral parameter is set
		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitShort)
		defer cancel()

		workspace, err := client.WorkspaceByOwnerAndName(ctx, workspace.OwnerName, workspace.Name, wirtualsdk.WorkspaceOptions{})
		require.NoError(t, err)
		actualParameters, err := client.WorkspaceBuildParameters(ctx, workspace.LatestBuild.ID)
		require.NoError(t, err)
		require.Contains(t, actualParameters, wirtualsdk.WorkspaceBuildParameter{
			Name:  ephemeralParameterName,
			Value: ephemeralParameterValue,
		})
	})

	t.Run("EphemeralParameterFlags", func(t *testing.T) {
		t.Parallel()

		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
		owner := wirtualdtest.CreateFirstUser(t, client)
		member, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, echoResponses)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID)
		workspace := wirtualdtest.CreateWorkspace(t, member, template.ID)
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)
		// Stop the workspace
		workspaceBuild := wirtualdtest.CreateWorkspaceBuild(t, client, workspace, database.WorkspaceTransitionStop)
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspaceBuild.ID)

		inv, root := clitest.New(t, "start", workspace.Name,
			"--ephemeral-parameter", fmt.Sprintf("%s=%s", ephemeralParameterName, ephemeralParameterValue))
		clitest.SetupConfig(t, member, root)
		doneChan := make(chan struct{})
		pty := ptytest.New(t).Attach(inv)
		go func() {
			defer close(doneChan)
			err := inv.Run()
			assert.NoError(t, err)
		}()

		pty.ExpectMatch("workspace has been started")
		<-doneChan

		// Verify if ephemeral parameter is set
		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitShort)
		defer cancel()

		workspace, err := client.WorkspaceByOwnerAndName(ctx, workspace.OwnerName, workspace.Name, wirtualsdk.WorkspaceOptions{})
		require.NoError(t, err)
		actualParameters, err := client.WorkspaceBuildParameters(ctx, workspace.LatestBuild.ID)
		require.NoError(t, err)
		require.Contains(t, actualParameters, wirtualsdk.WorkspaceBuildParameter{
			Name:  ephemeralParameterName,
			Value: ephemeralParameterValue,
		})
	})
}

func TestStartWithParameters(t *testing.T) {
	t.Parallel()

	t.Run("DoNotAskForImmutables", func(t *testing.T) {
		t.Parallel()

		// Create the workspace
		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
		owner := wirtualdtest.CreateFirstUser(t, client)
		member, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, immutableParamsResponse)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID)
		workspace := wirtualdtest.CreateWorkspace(t, member, template.ID, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.RichParameterValues = []wirtualsdk.WorkspaceBuildParameter{
				{
					Name:  immutableParameterName,
					Value: immutableParameterValue,
				},
			}
		})
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)

		// Stop the workspace
		workspaceBuild := wirtualdtest.CreateWorkspaceBuild(t, client, workspace, database.WorkspaceTransitionStop)
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspaceBuild.ID)

		// Start the workspace again
		inv, root := clitest.New(t, "start", workspace.Name)
		clitest.SetupConfig(t, member, root)
		doneChan := make(chan struct{})
		pty := ptytest.New(t).Attach(inv)
		go func() {
			defer close(doneChan)
			err := inv.Run()
			assert.NoError(t, err)
		}()

		pty.ExpectMatch("workspace has been started")
		<-doneChan

		// Verify if immutable parameter is set
		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitShort)
		defer cancel()

		workspace, err := client.WorkspaceByOwnerAndName(ctx, workspace.OwnerName, workspace.Name, wirtualsdk.WorkspaceOptions{})
		require.NoError(t, err)
		actualParameters, err := client.WorkspaceBuildParameters(ctx, workspace.LatestBuild.ID)
		require.NoError(t, err)
		require.Contains(t, actualParameters, wirtualsdk.WorkspaceBuildParameter{
			Name:  immutableParameterName,
			Value: immutableParameterValue,
		})
	})

	t.Run("AlwaysPrompt", func(t *testing.T) {
		t.Parallel()

		// Create the workspace
		client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
		owner := wirtualdtest.CreateFirstUser(t, client)
		member, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID)
		version := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, mutableParamsResponse)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID)
		workspace := wirtualdtest.CreateWorkspace(t, member, template.ID, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.RichParameterValues = []wirtualsdk.WorkspaceBuildParameter{
				{
					Name:  mutableParameterName,
					Value: mutableParameterValue,
				},
			}
		})
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)

		// Stop the workspace
		workspaceBuild := wirtualdtest.CreateWorkspaceBuild(t, client, workspace, database.WorkspaceTransitionStop)
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspaceBuild.ID)

		// Start the workspace again
		inv, root := clitest.New(t, "start", workspace.Name, "--always-prompt")
		clitest.SetupConfig(t, member, root)
		doneChan := make(chan struct{})
		pty := ptytest.New(t).Attach(inv)
		go func() {
			defer close(doneChan)
			err := inv.Run()
			assert.NoError(t, err)
		}()

		newValue := "xyz"
		pty.ExpectMatch(mutableParameterName)
		pty.WriteLine(newValue)
		pty.ExpectMatch("workspace has been started")
		<-doneChan

		// Verify that the updated values are persisted.
		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitShort)
		defer cancel()

		workspace, err := client.WorkspaceByOwnerAndName(ctx, workspace.OwnerName, workspace.Name, wirtualsdk.WorkspaceOptions{})
		require.NoError(t, err)
		actualParameters, err := client.WorkspaceBuildParameters(ctx, workspace.LatestBuild.ID)
		require.NoError(t, err)
		require.Contains(t, actualParameters, wirtualsdk.WorkspaceBuildParameter{
			Name:  mutableParameterName,
			Value: newValue,
		})
	})
}

// TestStartAutoUpdate also tests restart since the flows are virtually identical.
func TestStartAutoUpdate(t *testing.T) {
	t.Parallel()

	const (
		stringParameterName  = "myparam"
		stringParameterValue = "abc"
	)

	stringRichParameters := []*proto.RichParameter{
		{Name: stringParameterName, Type: "string", Mutable: true, Required: true},
	}

	type testcase struct {
		Name string
		Cmd  string
	}

	cases := []testcase{
		{
			Name: "StartOK",
			Cmd:  "start",
		},
		{
			Name: "RestartOK",
			Cmd:  "restart",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()

			client := wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
			owner := wirtualdtest.CreateFirstUser(t, client)
			member, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID)
			version1 := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
			wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version1.ID)
			template := wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version1.ID)
			workspace := wirtualdtest.CreateWorkspace(t, member, template.ID, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
				cwr.AutomaticUpdates = wirtualsdk.AutomaticUpdatesAlways
			})
			wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)

			if c.Cmd == "start" {
				wirtualdtest.MustTransitionWorkspace(t, member, workspace.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop)
			}
			version2 := wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, prepareEchoResponses(stringRichParameters), func(ctvr *wirtualsdk.CreateTemplateVersionRequest) {
				ctvr.TemplateID = template.ID
			})
			wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version2.ID)
			wirtualdtest.UpdateActiveTemplateVersion(t, client, template.ID, version2.ID)

			inv, root := clitest.New(t, c.Cmd, "-y", workspace.Name)
			clitest.SetupConfig(t, member, root)
			doneChan := make(chan struct{})
			pty := ptytest.New(t).Attach(inv)
			go func() {
				defer close(doneChan)
				err := inv.Run()
				assert.NoError(t, err)
			}()

			pty.ExpectMatch(stringParameterName)
			pty.WriteLine(stringParameterValue)
			<-doneChan

			workspace = wirtualdtest.MustWorkspace(t, member, workspace.ID)
			require.Equal(t, version2.ID, workspace.LatestBuild.TemplateVersionID)
		})
	}
}

func TestStart_AlreadyRunning(t *testing.T) {
	t.Parallel()
	ctx := testutil.Context(t, testutil.WaitShort)

	client, db := wirtualdtest.NewWithDatabase(t, nil)
	owner := wirtualdtest.CreateFirstUser(t, client)
	memberClient, member := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID)
	r := dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
		OwnerID:        member.ID,
		OrganizationID: owner.OrganizationID,
	}).Do()

	inv, root := clitest.New(t, "start", r.Workspace.Name)
	clitest.SetupConfig(t, memberClient, root)
	doneChan := make(chan struct{})
	pty := ptytest.New(t).Attach(inv)
	go func() {
		defer close(doneChan)
		err := inv.Run()
		assert.NoError(t, err)
	}()

	pty.ExpectMatch("workspace is already running")
	_ = testutil.RequireRecvCtx(ctx, t, doneChan)
}

func TestStart_Starting(t *testing.T) {
	t.Parallel()
	ctx := testutil.Context(t, testutil.WaitShort)

	store, ps := dbtestutil.NewDB(t)
	client := wirtualdtest.New(t, &wirtualdtest.Options{Pubsub: ps, Database: store})
	owner := wirtualdtest.CreateFirstUser(t, client)
	memberClient, member := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID)
	r := dbfake.WorkspaceBuild(t, store, database.WorkspaceTable{
		OwnerID:        member.ID,
		OrganizationID: owner.OrganizationID,
	}).
		Starting().
		Do()

	inv, root := clitest.New(t, "start", r.Workspace.Name)
	clitest.SetupConfig(t, memberClient, root)
	doneChan := make(chan struct{})
	pty := ptytest.New(t).Attach(inv)
	go func() {
		defer close(doneChan)
		err := inv.Run()
		assert.NoError(t, err)
	}()

	pty.ExpectMatch("workspace is already starting")

	_ = dbfake.JobComplete(t, store, r.Build.JobID).Pubsub(ps).Do()
	pty.ExpectMatch("workspace has been started")

	_ = testutil.RequireRecvCtx(ctx, t, doneChan)
}
