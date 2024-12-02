package autobuild_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"cdr.dev/slog"
	"cdr.dev/slog/sloggers/slogtest"

	"github.com/coder/coder/v2/wirtuald/autobuild"
	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
	"github.com/coder/coder/v2/wirtuald/database"
	"github.com/coder/coder/v2/wirtuald/database/dbauthz"
	"github.com/coder/coder/v2/wirtuald/notifications"
	"github.com/coder/coder/v2/wirtuald/notifications/notificationstest"
	"github.com/coder/coder/v2/wirtuald/schedule"
	"github.com/coder/coder/v2/wirtuald/schedule/cron"
	"github.com/coder/coder/v2/wirtuald/util/ptr"
	"github.com/coder/coder/v2/wirtualsdk"
	"github.com/coder/coder/v2/provisioner/echo"
	"github.com/coder/coder/v2/provisionersdk/proto"
	"github.com/coder/coder/v2/testutil"
)

func TestExecutorAutostartOK(t *testing.T) {
	t.Parallel()

	var (
		sched   = mustSchedule(t, "CRON_TZ=UTC 0 * * * *")
		tickCh  = make(chan time.Time)
		statsCh = make(chan autobuild.Stats)
		client  = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh,
		})
		// Given: we have a user with a workspace that has autostart enabled
		workspace = mustProvisionWorkspace(t, client, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.AutostartSchedule = ptr.Ref(sched.String())
		})
	)
	// Given: workspace is stopped
	workspace = wirtualdtest.MustTransitionWorkspace(t, client, workspace.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop)

	// When: the autobuild executor ticks after the scheduled time
	go func() {
		tickCh <- sched.Next(workspace.LatestBuild.CreatedAt)
		close(tickCh)
	}()

	// Then: the workspace should eventually be started
	stats := <-statsCh
	assert.Len(t, stats.Errors, 0)
	assert.Len(t, stats.Transitions, 1)
	assert.Contains(t, stats.Transitions, workspace.ID)
	assert.Equal(t, database.WorkspaceTransitionStart, stats.Transitions[workspace.ID])

	workspace = wirtualdtest.MustWorkspace(t, client, workspace.ID)
	assert.Equal(t, wirtualsdk.BuildReasonAutostart, workspace.LatestBuild.Reason)
	// Assert some template props. If this is not set correctly, the test
	// will fail.
	ctx := testutil.Context(t, testutil.WaitShort)
	template, err := client.Template(ctx, workspace.TemplateID)
	require.NoError(t, err)
	require.Equal(t, template.AutostartRequirement.DaysOfWeek, []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"})
}

func TestExecutorAutostartTemplateUpdated(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                 string
		automaticUpdates     wirtualsdk.AutomaticUpdates
		compatibleParameters bool
		expectStart          bool
		expectUpdate         bool
		expectNotification   bool
	}{
		{
			name:                 "Never",
			automaticUpdates:     wirtualsdk.AutomaticUpdatesNever,
			compatibleParameters: true,
			expectStart:          true,
			expectUpdate:         false,
		},
		{
			name:                 "Always_Compatible",
			automaticUpdates:     wirtualsdk.AutomaticUpdatesAlways,
			compatibleParameters: true,
			expectStart:          true,
			expectUpdate:         true,
			expectNotification:   true,
		},
		{
			name:                 "Always_Incompatible",
			automaticUpdates:     wirtualsdk.AutomaticUpdatesAlways,
			compatibleParameters: false,
			expectStart:          false,
			expectUpdate:         false,
		},
	}
	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var (
				sched    = mustSchedule(t, "CRON_TZ=UTC 0 * * * *")
				ctx      = context.Background()
				err      error
				tickCh   = make(chan time.Time)
				statsCh  = make(chan autobuild.Stats)
				logger   = slogtest.Make(t, &slogtest.Options{IgnoreErrors: !tc.expectStart}).Leveled(slog.LevelDebug)
				enqueuer = notificationstest.FakeEnqueuer{}
				client   = wirtualdtest.New(t, &wirtualdtest.Options{
					AutobuildTicker:          tickCh,
					IncludeProvisionerDaemon: true,
					AutobuildStats:           statsCh,
					Logger:                   &logger,
					NotificationsEnqueuer:    &enqueuer,
				})
				// Given: we have a user with a workspace that has autostart enabled
				workspace = mustProvisionWorkspace(t, client, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
					cwr.AutostartSchedule = ptr.Ref(sched.String())
					// Given: automatic updates from the test case
					cwr.AutomaticUpdates = tc.automaticUpdates
				})
			)
			// Given: workspace is stopped
			workspace = wirtualdtest.MustTransitionWorkspace(
				t, client, workspace.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop)

			orgs, err := client.OrganizationsByUser(ctx, workspace.OwnerID.String())
			require.NoError(t, err)
			require.Len(t, orgs, 1)

			var res *echo.Responses
			if !tc.compatibleParameters {
				// Given, parameters of the new version are not compatible.
				// Since initial version has no parameters, any parameters in the new version will be incompatible
				res = &echo.Responses{
					Parse: echo.ParseComplete,
					ProvisionApply: []*proto.Response{{
						Type: &proto.Response_Apply{
							Apply: &proto.ApplyComplete{
								Parameters: []*proto.RichParameter{
									{
										Name:     "new",
										Mutable:  false,
										Required: true,
									},
								},
							},
						},
					}},
				}
			}

			// Given: the workspace template has been updated
			newVersion := wirtualdtest.UpdateTemplateVersion(t, client, orgs[0].ID, res, workspace.TemplateID)
			wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, newVersion.ID)
			require.NoError(t, client.UpdateActiveTemplateVersion(
				ctx, workspace.TemplateID, wirtualsdk.UpdateActiveTemplateVersion{
					ID: newVersion.ID,
				},
			))

			t.Log("sending autobuild tick")
			// When: the autobuild executor ticks after the scheduled time
			go func() {
				tickCh <- sched.Next(workspace.LatestBuild.CreatedAt)
				close(tickCh)
			}()

			stats := <-statsCh
			if !tc.expectStart {
				// Then: the workspace should not be started
				assert.Len(t, stats.Transitions, 0)
				assert.Len(t, stats.Errors, 1)
				return
			}

			assert.Len(t, stats.Errors, 0)
			// Then: the workspace should be started
			assert.Len(t, stats.Transitions, 1)
			assert.Contains(t, stats.Transitions, workspace.ID)
			assert.Equal(t, database.WorkspaceTransitionStart, stats.Transitions[workspace.ID])
			ws := wirtualdtest.MustWorkspace(t, client, workspace.ID)
			if tc.expectUpdate {
				// Then: uses the updated version
				assert.Equal(t, newVersion.ID, ws.LatestBuild.TemplateVersionID,
					"expected workspace build to be using the updated template version")
			} else {
				// Then: uses the previous template version
				assert.Equal(t, workspace.LatestBuild.TemplateVersionID, ws.LatestBuild.TemplateVersionID,
					"expected workspace build to be using the old template version")
			}

			if tc.expectNotification {
				sent := enqueuer.Sent()
				require.Len(t, sent, 1)
				require.Equal(t, sent[0].UserID, workspace.OwnerID)
				require.Contains(t, sent[0].Targets, workspace.TemplateID)
				require.Contains(t, sent[0].Targets, workspace.ID)
				require.Contains(t, sent[0].Targets, workspace.OrganizationID)
				require.Contains(t, sent[0].Targets, workspace.OwnerID)
				require.Equal(t, newVersion.Name, sent[0].Labels["template_version_name"])
				require.Equal(t, "autobuild", sent[0].Labels["initiator"])
				require.Equal(t, "autostart", sent[0].Labels["reason"])
			} else {
				require.Empty(t, enqueuer.Sent())
			}
		})
	}
}

func TestExecutorAutostartAlreadyRunning(t *testing.T) {
	t.Parallel()

	var (
		sched   = mustSchedule(t, "CRON_TZ=UTC 0 * * * *")
		tickCh  = make(chan time.Time)
		statsCh = make(chan autobuild.Stats)
		client  = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh,
		})
		// Given: we have a user with a workspace that has autostart enabled
		workspace = mustProvisionWorkspace(t, client, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.AutostartSchedule = ptr.Ref(sched.String())
		})
	)

	// Given: we ensure the workspace is running
	require.Equal(t, wirtualsdk.WorkspaceTransitionStart, workspace.LatestBuild.Transition)

	// When: the autobuild executor ticks
	go func() {
		tickCh <- sched.Next(workspace.LatestBuild.CreatedAt)
		close(tickCh)
	}()

	// Then: the workspace should not be started.
	stats := <-statsCh
	assert.Len(t, stats.Errors, 0)
	require.Len(t, stats.Transitions, 0)
}

func TestExecutorAutostartNotEnabled(t *testing.T) {
	t.Parallel()

	var (
		tickCh  = make(chan time.Time)
		statsCh = make(chan autobuild.Stats)
		client  = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh,
		})
		// Given: we have a user with a workspace that does not have autostart enabled
		workspace = mustProvisionWorkspace(t, client, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.AutostartSchedule = nil
		})
	)

	// Given: workspace does not have autostart enabled
	require.Empty(t, workspace.AutostartSchedule)

	// Given: workspace is stopped
	workspace = wirtualdtest.MustTransitionWorkspace(t, client, workspace.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop)

	// When: the autobuild executor ticks way into the future
	go func() {
		tickCh <- workspace.LatestBuild.CreatedAt.Add(24 * time.Hour)
		close(tickCh)
	}()

	// Then: the workspace should not be started.
	stats := <-statsCh
	assert.Len(t, stats.Errors, 0)
	require.Len(t, stats.Transitions, 0)
}

func TestExecutorAutostartUserSuspended(t *testing.T) {
	t.Parallel()

	var (
		ctx     = testutil.Context(t, testutil.WaitShort)
		sched   = mustSchedule(t, "CRON_TZ=UTC 0 * * * *")
		tickCh  = make(chan time.Time)
		statsCh = make(chan autobuild.Stats)
		client  = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh,
		})
	)

	admin := wirtualdtest.CreateFirstUser(t, client)
	version := wirtualdtest.CreateTemplateVersion(t, client, admin.OrganizationID, nil)
	wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
	template := wirtualdtest.CreateTemplate(t, client, admin.OrganizationID, version.ID)
	userClient, user := wirtualdtest.CreateAnotherUser(t, client, admin.OrganizationID)
	workspace := wirtualdtest.CreateWorkspace(t, userClient, template.ID, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
		cwr.AutostartSchedule = ptr.Ref(sched.String())
	})
	wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, userClient, workspace.LatestBuild.ID)
	workspace = wirtualdtest.MustWorkspace(t, userClient, workspace.ID)

	// Given: workspace is stopped, and the user is suspended.
	workspace = wirtualdtest.MustTransitionWorkspace(t, userClient, workspace.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop)

	_, err := client.UpdateUserStatus(ctx, user.ID.String(), wirtualsdk.UserStatusSuspended)
	require.NoError(t, err, "update user status")

	// When: the autobuild executor ticks after the scheduled time
	go func() {
		tickCh <- sched.Next(workspace.LatestBuild.CreatedAt)
		close(tickCh)
	}()

	// Then: nothing should happen
	stats := testutil.RequireRecvCtx(ctx, t, statsCh)
	assert.Len(t, stats.Errors, 0)
	assert.Len(t, stats.Transitions, 0)
}

func TestExecutorAutostopOK(t *testing.T) {
	t.Parallel()

	var (
		tickCh  = make(chan time.Time)
		statsCh = make(chan autobuild.Stats)
		client  = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh,
		})
		// Given: we have a user with a workspace
		workspace = mustProvisionWorkspace(t, client)
	)
	// Given: workspace is running
	require.Equal(t, wirtualsdk.WorkspaceTransitionStart, workspace.LatestBuild.Transition)
	require.NotZero(t, workspace.LatestBuild.Deadline)

	// When: the autobuild executor ticks *after* the deadline:
	go func() {
		tickCh <- workspace.LatestBuild.Deadline.Time.Add(time.Minute)
		close(tickCh)
	}()

	// Then: the workspace should be stopped
	stats := <-statsCh
	assert.Len(t, stats.Errors, 0)
	assert.Len(t, stats.Transitions, 1)
	assert.Contains(t, stats.Transitions, workspace.ID)
	assert.Equal(t, database.WorkspaceTransitionStop, stats.Transitions[workspace.ID])

	workspace = wirtualdtest.MustWorkspace(t, client, workspace.ID)
	assert.Equal(t, wirtualsdk.BuildReasonAutostop, workspace.LatestBuild.Reason)
}

func TestExecutorAutostopExtend(t *testing.T) {
	t.Parallel()

	var (
		ctx     = context.Background()
		tickCh  = make(chan time.Time)
		statsCh = make(chan autobuild.Stats)
		client  = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh,
		})
		// Given: we have a user with a workspace
		workspace        = mustProvisionWorkspace(t, client)
		originalDeadline = workspace.LatestBuild.Deadline
	)
	// Given: workspace is running
	require.Equal(t, wirtualsdk.WorkspaceTransitionStart, workspace.LatestBuild.Transition)
	require.NotZero(t, originalDeadline)

	// Given: we extend the workspace deadline
	newDeadline := originalDeadline.Time.Add(30 * time.Minute)
	err := client.PutExtendWorkspace(ctx, workspace.ID, wirtualsdk.PutExtendWorkspaceRequest{
		Deadline: newDeadline,
	})
	require.NoError(t, err, "extend workspace deadline")

	// When: the autobuild executor ticks *after* the original deadline:
	go func() {
		tickCh <- originalDeadline.Time.Add(time.Minute)
	}()

	// Then: nothing should happen and the workspace should stay running
	stats := <-statsCh
	assert.Len(t, stats.Errors, 0)
	assert.Len(t, stats.Transitions, 0)

	// When: the autobuild executor ticks after the *new* deadline:
	go func() {
		tickCh <- newDeadline.Add(time.Minute)
		close(tickCh)
	}()

	// Then: the workspace should be stopped
	stats = <-statsCh
	assert.Len(t, stats.Errors, 0)
	assert.Len(t, stats.Transitions, 1)
	assert.Contains(t, stats.Transitions, workspace.ID)
	assert.Equal(t, database.WorkspaceTransitionStop, stats.Transitions[workspace.ID])
}

func TestExecutorAutostopAlreadyStopped(t *testing.T) {
	t.Parallel()

	var (
		tickCh  = make(chan time.Time)
		statsCh = make(chan autobuild.Stats)
		client  = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh,
		})
		// Given: we have a user with a workspace (disabling autostart)
		workspace = mustProvisionWorkspace(t, client, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.AutostartSchedule = nil
		})
	)

	// Given: workspace is stopped
	workspace = wirtualdtest.MustTransitionWorkspace(t, client, workspace.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop)

	// When: the autobuild executor ticks past the TTL
	go func() {
		tickCh <- workspace.LatestBuild.Deadline.Time.Add(time.Minute)
		close(tickCh)
	}()

	// Then: the workspace should remain stopped and no build should happen.
	stats := <-statsCh
	assert.Len(t, stats.Errors, 0)
	assert.Len(t, stats.Transitions, 0)
}

func TestExecutorAutostopNotEnabled(t *testing.T) {
	t.Parallel()

	var (
		tickCh  = make(chan time.Time)
		statsCh = make(chan autobuild.Stats)
		client  = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh,
		})
		// Given: we have a user with a workspace
		workspace = mustProvisionWorkspace(t, client, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.TTLMillis = nil
		})
	)

	// Given: workspace has no TTL set
	workspace = wirtualdtest.MustWorkspace(t, client, workspace.ID)
	require.Nil(t, workspace.TTLMillis)
	require.Zero(t, workspace.LatestBuild.Deadline)
	require.NotZero(t, workspace.LatestBuild.Job.CompletedAt)

	// Given: workspace is running
	require.Equal(t, wirtualsdk.WorkspaceTransitionStart, workspace.LatestBuild.Transition)

	// When: the autobuild executor ticks a year in the future
	go func() {
		tickCh <- workspace.LatestBuild.Job.CompletedAt.AddDate(1, 0, 0)
		close(tickCh)
	}()

	// Then: the workspace should not be stopped.
	stats := <-statsCh
	assert.Len(t, stats.Errors, 0)
	assert.Len(t, stats.Transitions, 0)
}

func TestExecutorWorkspaceDeleted(t *testing.T) {
	t.Parallel()

	var (
		sched   = mustSchedule(t, "CRON_TZ=UTC 0 * * * *")
		tickCh  = make(chan time.Time)
		statsCh = make(chan autobuild.Stats)
		client  = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh,
		})
		// Given: we have a user with a workspace that has autostart enabled
		workspace = mustProvisionWorkspace(t, client, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.AutostartSchedule = ptr.Ref(sched.String())
		})
	)

	// Given: workspace is deleted
	workspace = wirtualdtest.MustTransitionWorkspace(t, client, workspace.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionDelete)

	// When: the autobuild executor ticks
	go func() {
		tickCh <- sched.Next(workspace.LatestBuild.CreatedAt)
		close(tickCh)
	}()

	// Then: nothing should happen
	stats := <-statsCh
	assert.Len(t, stats.Errors, 0)
	assert.Len(t, stats.Transitions, 0)
}

func TestExecutorWorkspaceAutostartTooEarly(t *testing.T) {
	t.Parallel()

	var (
		sched   = mustSchedule(t, "CRON_TZ=UTC 0 * * * *")
		tickCh  = make(chan time.Time)
		statsCh = make(chan autobuild.Stats)
		client  = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh,
		})
		// futureTime     = time.Now().Add(time.Hour)
		// futureTimeCron = fmt.Sprintf("%d %d * * *", futureTime.Minute(), futureTime.Hour())
		// Given: we have a user with a workspace configured to autostart some time in the future
		workspace = mustProvisionWorkspace(t, client, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.AutostartSchedule = ptr.Ref(sched.String())
		})
	)

	// When: the autobuild executor ticks before the next scheduled time
	go func() {
		tickCh <- sched.Next(workspace.LatestBuild.CreatedAt).Add(-time.Minute)
		close(tickCh)
	}()

	// Then: nothing should happen
	stats := <-statsCh
	assert.Len(t, stats.Errors, 0)
	assert.Len(t, stats.Transitions, 0)
}

func TestExecutorWorkspaceAutostopBeforeDeadline(t *testing.T) {
	t.Parallel()

	var (
		tickCh  = make(chan time.Time)
		statsCh = make(chan autobuild.Stats)
		client  = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh,
		})
		// Given: we have a user with a workspace
		workspace = mustProvisionWorkspace(t, client)
	)

	// Given: workspace is running and has a non-zero deadline
	require.Equal(t, wirtualsdk.WorkspaceTransitionStart, workspace.LatestBuild.Transition)
	require.NotZero(t, workspace.LatestBuild.Deadline)

	// When: the autobuild executor ticks before the TTL
	go func() {
		tickCh <- workspace.LatestBuild.Deadline.Time.Add(-1 * time.Minute)
		close(tickCh)
	}()

	// Then: nothing should happen
	stats := <-statsCh
	assert.Len(t, stats.Errors, 0)
	assert.Len(t, stats.Transitions, 0)
}

func TestExecuteAutostopSuspendedUser(t *testing.T) {
	t.Parallel()

	var (
		ctx     = testutil.Context(t, testutil.WaitShort)
		tickCh  = make(chan time.Time)
		statsCh = make(chan autobuild.Stats)
		client  = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh,
		})
	)

	admin := wirtualdtest.CreateFirstUser(t, client)
	version := wirtualdtest.CreateTemplateVersion(t, client, admin.OrganizationID, nil)
	wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
	template := wirtualdtest.CreateTemplate(t, client, admin.OrganizationID, version.ID)
	userClient, user := wirtualdtest.CreateAnotherUser(t, client, admin.OrganizationID)
	workspace := wirtualdtest.CreateWorkspace(t, userClient, template.ID)
	wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, userClient, workspace.LatestBuild.ID)

	// Given: workspace is running, and the user is suspended.
	workspace = wirtualdtest.MustWorkspace(t, userClient, workspace.ID)
	require.Equal(t, wirtualsdk.WorkspaceStatusRunning, workspace.LatestBuild.Status)
	_, err := client.UpdateUserStatus(ctx, user.ID.String(), wirtualsdk.UserStatusSuspended)
	require.NoError(t, err, "update user status")

	// When: the autobuild executor ticks after the scheduled time
	go func() {
		tickCh <- time.Unix(0, 0) // the exact time is not important
		close(tickCh)
	}()

	// Then: the workspace should be stopped
	stats := <-statsCh
	assert.Len(t, stats.Errors, 0)
	assert.Len(t, stats.Transitions, 1)
	assert.Equal(t, stats.Transitions[workspace.ID], database.WorkspaceTransitionStop)

	// Wait for stop to complete
	workspace = wirtualdtest.MustWorkspace(t, client, workspace.ID)
	workspaceBuild := wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)
	assert.Equal(t, wirtualsdk.WorkspaceStatusStopped, workspaceBuild.Status)
}

func TestExecutorWorkspaceAutostopNoWaitChangedMyMind(t *testing.T) {
	t.Parallel()

	var (
		ctx     = context.Background()
		tickCh  = make(chan time.Time)
		statsCh = make(chan autobuild.Stats)
		client  = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh,
		})
		// Given: we have a user with a workspace
		workspace = mustProvisionWorkspace(t, client)
	)

	// Given: the user changes their mind and decides their workspace should not autostop
	err := client.UpdateWorkspaceTTL(ctx, workspace.ID, wirtualsdk.UpdateWorkspaceTTLRequest{TTLMillis: nil})
	require.NoError(t, err)

	// Then: the deadline should still be the original value
	updated := wirtualdtest.MustWorkspace(t, client, workspace.ID)
	assert.WithinDuration(t, workspace.LatestBuild.Deadline.Time, updated.LatestBuild.Deadline.Time, time.Minute)

	// When: the autobuild executor ticks after the original deadline
	go func() {
		tickCh <- workspace.LatestBuild.Deadline.Time.Add(time.Minute)
	}()

	// Then: the workspace should stop
	stats := <-statsCh
	assert.Len(t, stats.Errors, 0)
	assert.Len(t, stats.Transitions, 1)
	assert.Equal(t, stats.Transitions[workspace.ID], database.WorkspaceTransitionStop)

	// Wait for stop to complete
	updated = wirtualdtest.MustWorkspace(t, client, workspace.ID)
	_ = wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, updated.LatestBuild.ID)

	// Start the workspace again
	workspace = wirtualdtest.MustTransitionWorkspace(t, client, workspace.ID, database.WorkspaceTransitionStop, database.WorkspaceTransitionStart)

	// Given: the user changes their mind again and wants to enable autostop
	newTTL := 8 * time.Hour
	err = client.UpdateWorkspaceTTL(ctx, workspace.ID, wirtualsdk.UpdateWorkspaceTTLRequest{TTLMillis: ptr.Ref(newTTL.Milliseconds())})
	require.NoError(t, err)

	// Then: the deadline should remain at the zero value
	updated = wirtualdtest.MustWorkspace(t, client, workspace.ID)
	assert.Zero(t, updated.LatestBuild.Deadline)

	// When: the relentless onward march of time continues
	go func() {
		tickCh <- workspace.LatestBuild.Deadline.Time.Add(newTTL + time.Minute)
		close(tickCh)
	}()

	// Then: the workspace should not stop
	stats = <-statsCh
	assert.Len(t, stats.Errors, 0)
	assert.Len(t, stats.Transitions, 0)
}

func TestExecutorAutostartMultipleOK(t *testing.T) {
	if os.Getenv("DB") == "" {
		t.Skip(`This test only really works when using a "real" database, similar to a HA setup`)
	}

	t.Parallel()

	var (
		sched    = mustSchedule(t, "CRON_TZ=UTC 0 * * * *")
		tickCh   = make(chan time.Time)
		tickCh2  = make(chan time.Time)
		statsCh1 = make(chan autobuild.Stats)
		statsCh2 = make(chan autobuild.Stats)
		client   = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh1,
		})
		_ = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh2,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh2,
		})
		// Given: we have a user with a workspace that has autostart enabled (default)
		workspace = mustProvisionWorkspace(t, client, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.AutostartSchedule = ptr.Ref(sched.String())
		})
	)
	// Given: workspace is stopped
	workspace = wirtualdtest.MustTransitionWorkspace(t, client, workspace.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop)

	// When: the autobuild executor ticks past the scheduled time
	go func() {
		tickCh <- sched.Next(workspace.LatestBuild.CreatedAt)
		tickCh2 <- sched.Next(workspace.LatestBuild.CreatedAt)
		close(tickCh)
		close(tickCh2)
	}()

	// Then: the workspace should eventually be started
	stats1 := <-statsCh1
	assert.Len(t, stats1.Errors, 0)
	assert.Len(t, stats1.Transitions, 1)
	assert.Contains(t, stats1.Transitions, workspace.ID)
	assert.Equal(t, database.WorkspaceTransitionStart, stats1.Transitions[workspace.ID])

	// Then: the other executor should not have done anything
	stats2 := <-statsCh2
	assert.Len(t, stats2.Errors, 0)
	assert.Len(t, stats2.Transitions, 0)
}

func TestExecutorAutostartWithParameters(t *testing.T) {
	t.Parallel()

	const (
		stringParameterName  = "string_parameter"
		stringParameterValue = "abc"

		numberParameterName  = "number_parameter"
		numberParameterValue = "7"
	)

	var (
		sched   = mustSchedule(t, "CRON_TZ=UTC 0 * * * *")
		tickCh  = make(chan time.Time)
		statsCh = make(chan autobuild.Stats)
		client  = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh,
		})

		richParameters = []*proto.RichParameter{
			{Name: stringParameterName, Type: "string", Mutable: true},
			{Name: numberParameterName, Type: "number", Mutable: true},
		}

		// Given: we have a user with a workspace that has autostart enabled
		workspace = mustProvisionWorkspaceWithParameters(t, client, richParameters, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.AutostartSchedule = ptr.Ref(sched.String())
			cwr.RichParameterValues = []wirtualsdk.WorkspaceBuildParameter{
				{
					Name:  stringParameterName,
					Value: stringParameterValue,
				},
				{
					Name:  numberParameterName,
					Value: numberParameterValue,
				},
			}
		})
	)
	// Given: workspace is stopped
	workspace = wirtualdtest.MustTransitionWorkspace(t, client, workspace.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop)

	// When: the autobuild executor ticks after the scheduled time
	go func() {
		tickCh <- sched.Next(workspace.LatestBuild.CreatedAt)
		close(tickCh)
	}()

	// Then: the workspace with parameters should eventually be started
	stats := <-statsCh
	assert.Len(t, stats.Errors, 0)
	assert.Len(t, stats.Transitions, 1)
	assert.Contains(t, stats.Transitions, workspace.ID)
	assert.Equal(t, database.WorkspaceTransitionStart, stats.Transitions[workspace.ID])

	workspace = wirtualdtest.MustWorkspace(t, client, workspace.ID)
	mustWorkspaceParameters(t, client, workspace.LatestBuild.ID)
}

func TestExecutorAutostartTemplateDisabled(t *testing.T) {
	t.Parallel()

	var (
		sched   = mustSchedule(t, "CRON_TZ=UTC 0 * * * *")
		tickCh  = make(chan time.Time)
		statsCh = make(chan autobuild.Stats)

		client = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh,
			TemplateScheduleStore: schedule.MockTemplateScheduleStore{
				GetFn: func(_ context.Context, _ database.Store, _ uuid.UUID) (schedule.TemplateScheduleOptions, error) {
					return schedule.TemplateScheduleOptions{
						UserAutostartEnabled: false,
						UserAutostopEnabled:  true,
						DefaultTTL:           0,
						AutostopRequirement:  schedule.TemplateAutostopRequirement{},
					}, nil
				},
			},
		})
		// futureTime     = time.Now().Add(time.Hour)
		// futureTimeCron = fmt.Sprintf("%d %d * * *", futureTime.Minute(), futureTime.Hour())
		// Given: we have a user with a workspace configured to autostart some time in the future
		workspace = mustProvisionWorkspace(t, client, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.AutostartSchedule = ptr.Ref(sched.String())
		})
	)
	// Given: workspace is stopped
	workspace = wirtualdtest.MustTransitionWorkspace(t, client, workspace.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop)

	// When: the autobuild executor ticks before the next scheduled time
	go func() {
		tickCh <- sched.Next(workspace.LatestBuild.CreatedAt).Add(time.Minute)
		close(tickCh)
	}()

	// Then: nothing should happen
	stats := <-statsCh
	assert.Len(t, stats.Errors, 0)
	assert.Len(t, stats.Transitions, 0)
}

func TestExecutorAutostopTemplateDisabled(t *testing.T) {
	t.Parallel()

	// Given: we have a workspace built from a template that disallows user autostop
	var (
		tickCh  = make(chan time.Time)
		statsCh = make(chan autobuild.Stats)

		client = wirtualdtest.New(t, &wirtualdtest.Options{
			AutobuildTicker:          tickCh,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statsCh,
			// We are using a mock store here as the AGPL store does not implement this.
			TemplateScheduleStore: schedule.MockTemplateScheduleStore{
				GetFn: func(_ context.Context, _ database.Store, _ uuid.UUID) (schedule.TemplateScheduleOptions, error) {
					return schedule.TemplateScheduleOptions{
						UserAutostopEnabled: false,
						DefaultTTL:          time.Hour,
					}, nil
				},
			},
		})
		// Given: we have a user with a workspace configured to autostop 30 minutes in the future
		workspace = mustProvisionWorkspace(t, client, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.TTLMillis = ptr.Ref(30 * time.Minute.Milliseconds())
		})
	)

	// When: we create the workspace
	// Then: the deadline should be set to the template default TTL
	assert.WithinDuration(t, workspace.LatestBuild.CreatedAt.Add(time.Hour), workspace.LatestBuild.Deadline.Time, time.Minute)

	// When: the autobuild executor ticks after the workspace setting, but before the template setting:
	go func() {
		tickCh <- workspace.LatestBuild.Job.CompletedAt.Add(45 * time.Minute)
	}()

	// Then: nothing should happen
	stats := <-statsCh
	assert.Len(t, stats.Errors, 0)
	assert.Len(t, stats.Transitions, 0)

	// When: the autobuild executor ticks after the template setting:
	go func() {
		tickCh <- workspace.LatestBuild.Job.CompletedAt.Add(61 * time.Minute)
		close(tickCh)
	}()

	// Then: the workspace should be stopped
	stats = <-statsCh
	assert.Len(t, stats.Errors, 0)
	assert.Len(t, stats.Transitions, 1)
	assert.Contains(t, stats.Transitions, workspace.ID)
	assert.Equal(t, database.WorkspaceTransitionStop, stats.Transitions[workspace.ID])
}

// Test that an AGPL AccessControlStore properly disables
// functionality.
func TestExecutorRequireActiveVersion(t *testing.T) {
	t.Parallel()

	var (
		sched  = mustSchedule(t, "CRON_TZ=UTC 0 * * * *")
		ticker = make(chan time.Time)
		statCh = make(chan autobuild.Stats)

		ownerClient, db = wirtualdtest.NewWithDatabase(t, &wirtualdtest.Options{
			AutobuildTicker:          ticker,
			IncludeProvisionerDaemon: true,
			AutobuildStats:           statCh,
			TemplateScheduleStore:    schedule.NewAGPLTemplateScheduleStore(),
		})
	)
	ctx := testutil.Context(t, testutil.WaitShort)
	owner := wirtualdtest.CreateFirstUser(t, ownerClient)
	me, err := ownerClient.User(ctx, wirtualsdk.Me)
	require.NoError(t, err)

	// Create an active and inactive template version. We'll
	// build a regular member's workspace using a non-active
	// template version and assert that the field is not abided
	// since there is no enterprise license.
	activeVersion := wirtualdtest.CreateTemplateVersion(t, ownerClient, owner.OrganizationID, nil)
	wirtualdtest.AwaitTemplateVersionJobCompleted(t, ownerClient, activeVersion.ID)
	template := wirtualdtest.CreateTemplate(t, ownerClient, owner.OrganizationID, activeVersion.ID)
	//nolint We need to set this in the database directly, because the API will return an error
	// letting you know that this feature requires an enterprise license.
	err = db.UpdateTemplateAccessControlByID(dbauthz.As(ctx, wirtualdtest.AuthzUserSubject(me, owner.OrganizationID)), database.UpdateTemplateAccessControlByIDParams{
		ID:                   template.ID,
		RequireActiveVersion: true,
	})
	require.NoError(t, err)
	inactiveVersion := wirtualdtest.CreateTemplateVersion(t, ownerClient, owner.OrganizationID, nil, func(ctvr *wirtualsdk.CreateTemplateVersionRequest) {
		ctvr.TemplateID = template.ID
	})
	wirtualdtest.AwaitTemplateVersionJobCompleted(t, ownerClient, inactiveVersion.ID)
	memberClient, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID)
	ws := wirtualdtest.CreateWorkspace(t, memberClient, uuid.Nil, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
		cwr.TemplateVersionID = inactiveVersion.ID
		cwr.AutostartSchedule = ptr.Ref(sched.String())
	})
	_ = wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, ownerClient, ws.LatestBuild.ID)
	ws = wirtualdtest.MustTransitionWorkspace(t, memberClient, ws.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop, func(req *wirtualsdk.CreateWorkspaceBuildRequest) {
		req.TemplateVersionID = inactiveVersion.ID
	})
	require.Equal(t, inactiveVersion.ID, ws.LatestBuild.TemplateVersionID)
	ticker <- sched.Next(ws.LatestBuild.CreatedAt)
	stats := <-statCh
	require.Len(t, stats.Transitions, 1)

	ws = wirtualdtest.MustWorkspace(t, memberClient, ws.ID)
	require.Equal(t, inactiveVersion.ID, ws.LatestBuild.TemplateVersionID)
}

// TestExecutorFailedWorkspace test AGPL functionality which mainly
// ensures that autostop actions as a result of a failed workspace
// build do not trigger.
// For enterprise functionality see enterprise/wirtuald/workspaces_test.go
func TestExecutorFailedWorkspace(t *testing.T) {
	t.Parallel()

	// Test that an AGPL TemplateScheduleStore properly disables
	// functionality.
	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		var (
			ticker = make(chan time.Time)
			statCh = make(chan autobuild.Stats)
			logger = slogtest.Make(t, &slogtest.Options{
				// We ignore errors here since we expect to fail
				// builds.
				IgnoreErrors: true,
			})
			failureTTL = time.Millisecond

			client = wirtualdtest.New(t, &wirtualdtest.Options{
				Logger:                   &logger,
				AutobuildTicker:          ticker,
				IncludeProvisionerDaemon: true,
				AutobuildStats:           statCh,
				TemplateScheduleStore:    schedule.NewAGPLTemplateScheduleStore(),
			})
		)
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
			Parse:          echo.ParseComplete,
			ProvisionPlan:  echo.PlanComplete,
			ProvisionApply: echo.ApplyFailed,
		})
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.FailureTTLMillis = ptr.Ref[int64](failureTTL.Milliseconds())
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		ws := wirtualdtest.CreateWorkspace(t, client, template.ID)
		build := wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, ws.LatestBuild.ID)
		require.Equal(t, wirtualsdk.WorkspaceStatusFailed, build.Status)
		ticker <- build.Job.CompletedAt.Add(failureTTL * 2)
		stats := <-statCh
		// Expect no transitions since we're using AGPL.
		require.Len(t, stats.Transitions, 0)
	})
}

// TestExecutorInactiveWorkspace test AGPL functionality which mainly
// ensures that autostop actions as a result of an inactive workspace
// do not trigger.
// For enterprise functionality see enterprise/wirtuald/workspaces_test.go
func TestExecutorInactiveWorkspace(t *testing.T) {
	t.Parallel()

	// Test that an AGPL TemplateScheduleStore properly disables
	// functionality.
	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		var (
			ticker = make(chan time.Time)
			statCh = make(chan autobuild.Stats)
			logger = slogtest.Make(t, &slogtest.Options{
				// We ignore errors here since we expect to fail
				// builds.
				IgnoreErrors: true,
			})
			inactiveTTL = time.Millisecond

			client = wirtualdtest.New(t, &wirtualdtest.Options{
				Logger:                   &logger,
				AutobuildTicker:          ticker,
				IncludeProvisionerDaemon: true,
				AutobuildStats:           statCh,
				TemplateScheduleStore:    schedule.NewAGPLTemplateScheduleStore(),
			})
		)
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
			Parse:          echo.ParseComplete,
			ProvisionPlan:  echo.PlanComplete,
			ProvisionApply: echo.ApplyComplete,
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.TimeTilDormantMillis = ptr.Ref[int64](inactiveTTL.Milliseconds())
		})
		ws := wirtualdtest.CreateWorkspace(t, client, template.ID)
		build := wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, ws.LatestBuild.ID)
		require.Equal(t, wirtualsdk.WorkspaceStatusRunning, build.Status)
		ticker <- ws.LastUsedAt.Add(inactiveTTL * 2)
		stats := <-statCh
		// Expect no transitions since we're using AGPL.
		require.Len(t, stats.Transitions, 0)
	})
}

func TestNotifications(t *testing.T) {
	t.Parallel()

	t.Run("Dormancy", func(t *testing.T) {
		t.Parallel()

		// Setup template with dormancy and create a workspace with it
		var (
			ticker         = make(chan time.Time)
			statCh         = make(chan autobuild.Stats)
			notifyEnq      = notificationstest.FakeEnqueuer{}
			timeTilDormant = time.Minute
			client         = wirtualdtest.New(t, &wirtualdtest.Options{
				AutobuildTicker:          ticker,
				AutobuildStats:           statCh,
				IncludeProvisionerDaemon: true,
				NotificationsEnqueuer:    &notifyEnq,
				TemplateScheduleStore: schedule.MockTemplateScheduleStore{
					GetFn: func(_ context.Context, _ database.Store, _ uuid.UUID) (schedule.TemplateScheduleOptions, error) {
						return schedule.TemplateScheduleOptions{
							UserAutostartEnabled: false,
							UserAutostopEnabled:  true,
							DefaultTTL:           0,
							AutostopRequirement:  schedule.TemplateAutostopRequirement{},
							TimeTilDormant:       timeTilDormant,
						}, nil
					},
				},
			})
			admin   = wirtualdtest.CreateFirstUser(t, client)
			version = wirtualdtest.CreateTemplateVersion(t, client, admin.OrganizationID, nil)
		)

		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		template := wirtualdtest.CreateTemplate(t, client, admin.OrganizationID, version.ID)
		userClient, _ := wirtualdtest.CreateAnotherUser(t, client, admin.OrganizationID)
		workspace := wirtualdtest.CreateWorkspace(t, userClient, template.ID)
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, userClient, workspace.LatestBuild.ID)

		// Stop workspace
		workspace = wirtualdtest.MustTransitionWorkspace(t, client, workspace.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop)
		_ = wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, userClient, workspace.LatestBuild.ID)

		// Wait for workspace to become dormant
		notifyEnq.Clear()
		ticker <- workspace.LastUsedAt.Add(timeTilDormant * 3)
		_ = testutil.RequireRecvCtx(testutil.Context(t, testutil.WaitShort), t, statCh)

		// Check that the workspace is dormant
		workspace = wirtualdtest.MustWorkspace(t, client, workspace.ID)
		require.NotNil(t, workspace.DormantAt)

		// Check that a notification was enqueued
		sent := notifyEnq.Sent()
		require.Len(t, sent, 1)
		require.Equal(t, sent[0].UserID, workspace.OwnerID)
		require.Equal(t, sent[0].TemplateID, notifications.TemplateWorkspaceDormant)
		require.Contains(t, sent[0].Targets, template.ID)
		require.Contains(t, sent[0].Targets, workspace.ID)
		require.Contains(t, sent[0].Targets, workspace.OrganizationID)
		require.Contains(t, sent[0].Targets, workspace.OwnerID)
	})
}

func mustProvisionWorkspace(t *testing.T, client *wirtualsdk.Client, mut ...func(*wirtualsdk.CreateWorkspaceRequest)) wirtualsdk.Workspace {
	t.Helper()
	user := wirtualdtest.CreateFirstUser(t, client)
	version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
	wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
	template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
	ws := wirtualdtest.CreateWorkspace(t, client, template.ID, mut...)
	wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, ws.LatestBuild.ID)
	return wirtualdtest.MustWorkspace(t, client, ws.ID)
}

func mustProvisionWorkspaceWithParameters(t *testing.T, client *wirtualsdk.Client, richParameters []*proto.RichParameter, mut ...func(*wirtualsdk.CreateWorkspaceRequest)) wirtualsdk.Workspace {
	t.Helper()
	user := wirtualdtest.CreateFirstUser(t, client)
	version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
		Parse: echo.ParseComplete,
		ProvisionPlan: []*proto.Response{
			{
				Type: &proto.Response_Plan{
					Plan: &proto.PlanComplete{
						Parameters: richParameters,
					},
				},
			},
		},
		ProvisionApply: echo.ApplyComplete,
	})
	wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
	template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
	ws := wirtualdtest.CreateWorkspace(t, client, template.ID, mut...)
	wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, ws.LatestBuild.ID)
	return wirtualdtest.MustWorkspace(t, client, ws.ID)
}

func mustSchedule(t *testing.T, s string) *cron.Schedule {
	t.Helper()
	sched, err := cron.Weekly(s)
	require.NoError(t, err)
	return sched
}

func mustWorkspaceParameters(t *testing.T, client *wirtualsdk.Client, workspaceID uuid.UUID) {
	ctx := testutil.Context(t, testutil.WaitShort)
	buildParameters, err := client.WorkspaceBuildParameters(ctx, workspaceID)
	require.NoError(t, err)
	require.NotEmpty(t, buildParameters)
}

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
