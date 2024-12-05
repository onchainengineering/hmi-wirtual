package wirtuald_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cdr.dev/slog"

	"cdr.dev/slog/sloggers/slogtest"

	entaudit "github.com/coder/coder/v2/enterprise/audit"
	"github.com/coder/coder/v2/enterprise/audit/backends"
	"github.com/coder/coder/v2/enterprise/wirtuald/license"
	"github.com/coder/coder/v2/enterprise/wirtuald/schedule"
	"github.com/coder/coder/v2/enterprise/wirtuald/wirtualdenttest"
	"github.com/coder/coder/v2/provisioner/echo"
	"github.com/coder/coder/v2/testutil"
	"github.com/coder/coder/v2/wirtuald/audit"
	"github.com/coder/coder/v2/wirtuald/autobuild"
	"github.com/coder/coder/v2/wirtuald/database"
	"github.com/coder/coder/v2/wirtuald/database/dbfake"
	"github.com/coder/coder/v2/wirtuald/database/dbtestutil"
	"github.com/coder/coder/v2/wirtuald/notifications"
	"github.com/coder/coder/v2/wirtuald/rbac"
	agplschedule "github.com/coder/coder/v2/wirtuald/schedule"
	"github.com/coder/coder/v2/wirtuald/schedule/cron"
	"github.com/coder/coder/v2/wirtuald/util/ptr"
	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
	"github.com/coder/coder/v2/wirtualsdk"
)

// agplUserQuietHoursScheduleStore is passed to
// NewEnterpriseTemplateScheduleStore as we don't care about updating the
// schedule and having it recalculate the build deadline in these tests.
func agplUserQuietHoursScheduleStore() *atomic.Pointer[agplschedule.UserQuietHoursScheduleStore] {
	store := agplschedule.NewAGPLUserQuietHoursScheduleStore()
	p := &atomic.Pointer[agplschedule.UserQuietHoursScheduleStore]{}
	p.Store(&store)
	return p
}

func TestCreateWorkspace(t *testing.T) {
	t.Parallel()

	t.Run("NoTemplateAccess", func(t *testing.T) {
		t.Parallel()

		client, first := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureTemplateRBAC:          1,
					wirtualsdk.FeatureMultipleOrganizations: 1,
				},
			},
		})

		other, _ := wirtualdtest.CreateAnotherUser(t, client, first.OrganizationID, rbac.RoleMember(), rbac.RoleOwner())

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		org, err := other.CreateOrganization(ctx, wirtualsdk.CreateOrganizationRequest{
			Name: "another",
		})
		require.NoError(t, err)
		version := wirtualdtest.CreateTemplateVersion(t, other, org.ID, nil)
		template := wirtualdtest.CreateTemplate(t, other, org.ID, version.ID)

		_, err = client.CreateWorkspace(ctx, first.OrganizationID, wirtualsdk.Me, wirtualsdk.CreateWorkspaceRequest{
			TemplateID: template.ID,
			Name:       "workspace",
		})
		require.Error(t, err)
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusNotAcceptable, apiErr.StatusCode())
	})

	// Test that a user cannot indirectly access
	// a template they do not have access to.
	t.Run("Unauthorized", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		templateAdminClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		acl, err := templateAdminClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)

		require.Len(t, acl.Groups, 1)
		require.Len(t, acl.Users, 0)

		err = templateAdminClient.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			GroupPerms: map[string]wirtualsdk.TemplateRole{
				acl.Groups[0].ID.String(): wirtualsdk.TemplateRoleDeleted,
			},
		})
		require.NoError(t, err)

		client1, user1 := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)

		_, err = client1.Template(ctx, template.ID)
		require.Error(t, err)
		cerr, ok := wirtualsdk.AsError(err)
		require.True(t, ok)
		require.Equal(t, http.StatusNotFound, cerr.StatusCode())

		req := wirtualsdk.CreateWorkspaceRequest{
			TemplateID:        template.ID,
			Name:              "testme",
			AutostartSchedule: ptr.Ref("CRON_TZ=US/Central 30 9 * * 1-5"),
			TTLMillis:         ptr.Ref((8 * time.Hour).Milliseconds()),
		}

		_, err = client1.CreateWorkspace(ctx, user.OrganizationID, user1.ID.String(), req)
		require.Error(t, err)
	})

	t.Run("NoTemplateAccess", func(t *testing.T) {
		t.Parallel()
		ownerClient, owner := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				IncludeProvisionerDaemon: true,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureTemplateRBAC: 1,
				},
			},
		})

		templateAdmin, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID, rbac.RoleTemplateAdmin())
		user, _ := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID, rbac.RoleMember())

		version := wirtualdtest.CreateTemplateVersion(t, templateAdmin, owner.OrganizationID, nil)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, templateAdmin, version.ID)
		template := wirtualdtest.CreateTemplate(t, templateAdmin, owner.OrganizationID, version.ID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		// Remove everyone access
		err := templateAdmin.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			UserPerms: map[string]wirtualsdk.TemplateRole{},
			GroupPerms: map[string]wirtualsdk.TemplateRole{
				owner.OrganizationID.String(): wirtualsdk.TemplateRoleDeleted,
			},
		})
		require.NoError(t, err)

		// Test "everyone" access is revoked to the regular user
		_, err = user.Template(ctx, template.ID)
		require.Error(t, err)
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusNotFound, apiErr.StatusCode())

		_, err = user.CreateUserWorkspace(ctx, wirtualsdk.Me, wirtualsdk.CreateWorkspaceRequest{
			TemplateID:        template.ID,
			Name:              "random",
			AutostartSchedule: ptr.Ref("CRON_TZ=US/Central 30 9 * * 1-5"),
			TTLMillis:         ptr.Ref((8 * time.Hour).Milliseconds()),
			AutomaticUpdates:  wirtualsdk.AutomaticUpdatesNever,
		})
		require.Error(t, err)
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusBadRequest, apiErr.StatusCode())
		require.Contains(t, apiErr.Message, "doesn't exist")
	})
}

func TestCreateUserWorkspace(t *testing.T) {
	t.Parallel()

	t.Run("NoTemplateAccess", func(t *testing.T) {
		t.Parallel()

		client, first := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureTemplateRBAC:          1,
					wirtualsdk.FeatureMultipleOrganizations: 1,
				},
			},
		})

		other, _ := wirtualdtest.CreateAnotherUser(t, client, first.OrganizationID, rbac.RoleMember(), rbac.RoleOwner())

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		org, err := other.CreateOrganization(ctx, wirtualsdk.CreateOrganizationRequest{
			Name: "another",
		})
		require.NoError(t, err)
		version := wirtualdtest.CreateTemplateVersion(t, other, org.ID, nil)
		template := wirtualdtest.CreateTemplate(t, other, org.ID, version.ID)

		_, err = client.CreateUserWorkspace(ctx, wirtualsdk.Me, wirtualsdk.CreateWorkspaceRequest{
			TemplateID: template.ID,
			Name:       "workspace",
		})
		require.Error(t, err)
		var apiErr *wirtualsdk.Error
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusNotAcceptable, apiErr.StatusCode())
	})

	// Test that a user cannot indirectly access
	// a template they do not have access to.
	t.Run("Unauthorized", func(t *testing.T) {
		t.Parallel()

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		}})
		templateAdminClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		acl, err := templateAdminClient.TemplateACL(ctx, template.ID)
		require.NoError(t, err)

		require.Len(t, acl.Groups, 1)
		require.Len(t, acl.Users, 0)

		err = templateAdminClient.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
			GroupPerms: map[string]wirtualsdk.TemplateRole{
				acl.Groups[0].ID.String(): wirtualsdk.TemplateRoleDeleted,
			},
		})
		require.NoError(t, err)

		client1, user1 := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID)

		_, err = client1.Template(ctx, template.ID)
		require.Error(t, err)
		cerr, ok := wirtualsdk.AsError(err)
		require.True(t, ok)
		require.Equal(t, http.StatusNotFound, cerr.StatusCode())

		req := wirtualsdk.CreateWorkspaceRequest{
			TemplateID:        template.ID,
			Name:              "testme",
			AutostartSchedule: ptr.Ref("CRON_TZ=US/Central 30 9 * * 1-5"),
			TTLMillis:         ptr.Ref((8 * time.Hour).Milliseconds()),
		}

		_, err = client1.CreateUserWorkspace(ctx, user1.ID.String(), req)
		require.Error(t, err)
	})
}

func TestWorkspaceAutobuild(t *testing.T) {
	t.Parallel()

	t.Run("FailureTTLOK", func(t *testing.T) {
		t.Parallel()

		var (
			ticker = make(chan time.Time)
			statCh = make(chan autobuild.Stats)
			logger = slogtest.Make(t, &slogtest.Options{
				// We ignore errors here since we expect to fail
				// builds.
				IgnoreErrors: true,
			})
			failureTTL = time.Minute
		)

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				Logger:                   &logger,
				AutobuildTicker:          ticker,
				IncludeProvisionerDaemon: true,
				AutobuildStats:           statCh,
				TemplateScheduleStore:    schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{wirtualsdk.FeatureAdvancedTemplateScheduling: 1},
			},
		})

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
		// Expect workspace to transition to stopped state for breaching
		// failure TTL.
		require.Len(t, stats.Transitions, 1)
		require.Equal(t, stats.Transitions[ws.ID], database.WorkspaceTransitionStop)
	})

	t.Run("FailureTTLTooEarly", func(t *testing.T) {
		t.Parallel()

		var (
			ticker = make(chan time.Time)
			statCh = make(chan autobuild.Stats)
			logger = slogtest.Make(t, &slogtest.Options{
				// We ignore errors here since we expect to fail
				// builds.
				IgnoreErrors: true,
			})
			failureTTL = time.Minute
		)

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				Logger:                   &logger,
				AutobuildTicker:          ticker,
				IncludeProvisionerDaemon: true,
				AutobuildStats:           statCh,
				TemplateScheduleStore:    schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{wirtualsdk.FeatureAdvancedTemplateScheduling: 1},
			},
		})
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
		// Make it impossible to trigger the failure TTL.
		ticker <- build.Job.CompletedAt.Add(-failureTTL * 2)
		stats := <-statCh
		// Expect no transitions since not enough time has elapsed.
		require.Len(t, stats.Transitions, 0)
	})

	// This just provides a baseline that no actions are being taken
	// against a workspace when none of the TTL fields are set.
	t.Run("TemplateTTLsUnset", func(t *testing.T) {
		t.Parallel()

		var (
			ticker = make(chan time.Time)
			statCh = make(chan autobuild.Stats)
			logger = slogtest.Make(t, &slogtest.Options{
				// We ignore errors here since we expect to fail
				// builds.
				IgnoreErrors: true,
			})
		)

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				Logger:                   &logger,
				AutobuildTicker:          ticker,
				IncludeProvisionerDaemon: true,
				AutobuildStats:           statCh,
				TemplateScheduleStore:    schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{wirtualsdk.FeatureAdvancedTemplateScheduling: 1},
			},
		})
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
			Parse:          echo.ParseComplete,
			ProvisionPlan:  echo.PlanComplete,
			ProvisionApply: echo.ApplyComplete,
		})
		// Create a template without setting a failure_ttl.
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		require.Zero(t, template.TimeTilDormantMillis)
		require.Zero(t, template.FailureTTLMillis)
		require.Zero(t, template.TimeTilDormantAutoDeleteMillis)

		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		ws := wirtualdtest.CreateWorkspace(t, client, template.ID)
		build := wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, ws.LatestBuild.ID)
		require.Equal(t, wirtualsdk.WorkspaceStatusRunning, build.Status)
		ticker <- time.Now()
		stats := <-statCh
		// Expect no transitions since the fields are unset on the template.
		require.Len(t, stats.Transitions, 0)
	})

	t.Run("DormancyThresholdOK", func(t *testing.T) {
		t.Parallel()

		var (
			ctx           = testutil.Context(t, testutil.WaitMedium)
			ticker        = make(chan time.Time)
			statCh        = make(chan autobuild.Stats)
			inactiveTTL   = time.Minute
			auditRecorder = audit.NewMock()
		)

		logger := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true}).Leveled(slog.LevelDebug)

		client, db, user := wirtualdenttest.NewWithDatabase(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				AutobuildTicker:       ticker,
				AutobuildStats:        statCh,
				TemplateScheduleStore: schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
				Auditor:               auditRecorder,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{wirtualsdk.FeatureAdvancedTemplateScheduling: 1},
			},
		})

		tpl := dbfake.TemplateVersion(t, db).Seed(database.TemplateVersion{
			OrganizationID: user.OrganizationID,
			CreatedBy:      user.UserID,
		}).Do().Template

		template := wirtualdtest.UpdateTemplateMeta(t, client, tpl.ID, wirtualsdk.UpdateTemplateMeta{
			TimeTilDormantMillis: inactiveTTL.Milliseconds(),
		})

		resp := dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
			OrganizationID: user.OrganizationID,
			OwnerID:        user.UserID,
			TemplateID:     template.ID,
		}).Seed(database.WorkspaceBuild{
			Transition: database.WorkspaceTransitionStart,
		}).Do()
		require.Equal(t, database.WorkspaceTransitionStart, resp.Build.Transition)
		workspace := resp.Workspace

		auditRecorder.ResetLogs()
		// Simulate being inactive.
		ticker <- workspace.LastUsedAt.Add(inactiveTTL * 2)
		stats := <-statCh

		// Expect workspace to transition to stopped state for breaching
		// failure TTL.
		require.Len(t, stats.Transitions, 1)
		require.Equal(t, stats.Transitions[workspace.ID], database.WorkspaceTransitionStop)

		ws := wirtualdtest.MustWorkspace(t, client, workspace.ID)
		// Should be dormant now.
		require.NotNil(t, ws.DormantAt)
		// Should be transitioned to stop.
		require.Equal(t, wirtualsdk.WorkspaceTransitionStop, ws.LatestBuild.Transition)
		require.Len(t, auditRecorder.AuditLogs(), 1)
		alog := auditRecorder.AuditLogs()[0]
		require.Equal(t, int32(http.StatusOK), alog.StatusCode)
		require.Equal(t, database.AuditActionWrite, alog.Action)
		require.Equal(t, workspace.Name, alog.ResourceTarget)

		dormantLastUsedAt := ws.LastUsedAt
		// nolint:gocritic // this test is not testing RBAC.
		err := client.UpdateWorkspaceDormancy(ctx, ws.ID, wirtualsdk.UpdateWorkspaceDormancy{Dormant: false})
		require.NoError(t, err)

		// Assert that we updated our last_used_at so that we don't immediately
		// retrigger another lock action.
		ws = wirtualdtest.MustWorkspace(t, client, ws.ID)
		require.True(t, ws.LastUsedAt.After(dormantLastUsedAt))
	})

	// This test serves as a regression prevention for generating
	// audit logs in the same transaction the transition workspaces to
	// the dormant state. The auditor that is passed to autobuild does
	// not use the transaction when inserting an audit log which can
	// cause a deadlock.
	t.Run("NoDeadlock", func(t *testing.T) {
		t.Parallel()

		if !dbtestutil.WillUsePostgres() {
			t.Skipf("Skipping non-postgres run")
		}

		var (
			ticker      = make(chan time.Time)
			statCh      = make(chan autobuild.Stats)
			inactiveTTL = time.Minute
		)

		const (
			maxConns      = 3
			numWorkspaces = maxConns * 5
		)
		// This is a bit bizarre but necessary so that we can
		// initialize our wirtuald with a real auditor and limit DB connections
		// to simulate deadlock conditions.
		db, pubsub, sdb := dbtestutil.NewDBWithSQLDB(t)
		// Set MaxOpenConns so we can ensure we aren't inadvertently acquiring
		// another connection from within a transaction.
		sdb.SetMaxOpenConns(maxConns)
		auditor := entaudit.NewAuditor(db, entaudit.DefaultFilter, backends.NewPostgres(db, true))
		logger := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true}).Leveled(slog.LevelDebug)

		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				AutobuildTicker:          ticker,
				AutobuildStats:           statCh,
				TemplateScheduleStore:    schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
				Database:                 db,
				Pubsub:                   pubsub,
				Auditor:                  auditor,
				IncludeProvisionerDaemon: true,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{wirtualsdk.FeatureAdvancedTemplateScheduling: 1},
			},
		})

		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
			Parse:          echo.ParseComplete,
			ProvisionPlan:  echo.PlanComplete,
			ProvisionApply: echo.ApplyComplete,
		})
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.TimeTilDormantMillis = ptr.Ref[int64](inactiveTTL.Milliseconds())
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)

		workspaces := make([]wirtualsdk.Workspace, 0, numWorkspaces)
		for i := 0; i < numWorkspaces; i++ {
			ws := wirtualdtest.CreateWorkspace(t, client, template.ID)
			build := wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, ws.LatestBuild.ID)
			require.Equal(t, wirtualsdk.WorkspaceStatusRunning, build.Status)
			workspaces = append(workspaces, ws)
		}

		// Simulate being inactive.
		ticker <- time.Now().Add(time.Hour)
		stats := <-statCh

		// Expect workspace to transition to stopped state for breaching
		// failure TTL.
		require.Len(t, stats.Transitions, numWorkspaces)
		for _, ws := range workspaces {
			// The workspace should be dormant.
			ws = wirtualdtest.MustWorkspace(t, client, ws.ID)
			require.NotNil(t, ws.DormantAt)
		}
	})

	t.Run("DormancyThresholdTooEarly", func(t *testing.T) {
		t.Parallel()

		var (
			ticker      = make(chan time.Time)
			statCh      = make(chan autobuild.Stats)
			inactiveTTL = time.Minute
		)

		logger := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true}).Leveled(slog.LevelDebug)
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				AutobuildTicker:          ticker,
				IncludeProvisionerDaemon: true,
				AutobuildStats:           statCh,
				TemplateScheduleStore:    schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{wirtualsdk.FeatureAdvancedTemplateScheduling: 1},
			},
		})
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
			Parse:          echo.ParseComplete,
			ProvisionPlan:  echo.PlanComplete,
			ProvisionApply: echo.ApplyComplete,
		})
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.TimeTilDormantMillis = ptr.Ref[int64](inactiveTTL.Milliseconds())
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		ws := wirtualdtest.CreateWorkspace(t, client, template.ID)
		build := wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, ws.LatestBuild.ID)
		require.Equal(t, wirtualsdk.WorkspaceStatusRunning, build.Status)
		// Make it impossible to trigger the inactive ttl.
		ticker <- ws.LastUsedAt.Add(-inactiveTTL)
		stats := <-statCh
		// Expect no transitions since not enough time has elapsed.
		require.Len(t, stats.Transitions, 0)
	})

	// This is kind of a dumb test but it exists to offer some marginal
	// confidence that a bug in the auto-deletion logic doesn't delete running
	// workspaces.
	t.Run("ActiveWorkspacesNotDeleted", func(t *testing.T) {
		t.Parallel()

		var (
			ticker        = make(chan time.Time)
			statCh        = make(chan autobuild.Stats)
			autoDeleteTTL = time.Minute
		)

		logger := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true}).Leveled(slog.LevelDebug)
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				AutobuildTicker:          ticker,
				IncludeProvisionerDaemon: true,
				AutobuildStats:           statCh,
				TemplateScheduleStore:    schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{wirtualsdk.FeatureAdvancedTemplateScheduling: 1},
			},
		})
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
			Parse:          echo.ParseComplete,
			ProvisionPlan:  echo.PlanComplete,
			ProvisionApply: echo.ApplyComplete,
		})
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.TimeTilDormantAutoDeleteMillis = ptr.Ref[int64](autoDeleteTTL.Milliseconds())
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		ws := wirtualdtest.CreateWorkspace(t, client, template.ID)
		build := wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, ws.LatestBuild.ID)
		require.Nil(t, ws.DormantAt)
		require.Equal(t, wirtualsdk.WorkspaceStatusRunning, build.Status)
		ticker <- ws.LastUsedAt.Add(autoDeleteTTL * 2)
		stats := <-statCh
		// Expect no transitions since workspace is active.
		require.Len(t, stats.Transitions, 0)
	})

	// Assert that a stopped workspace that breaches the inactivity threshold
	// does not trigger a build transition but is still placed in the
	// dormant state.
	t.Run("InactiveStoppedWorkspaceNoTransition", func(t *testing.T) {
		t.Parallel()

		var (
			ticker      = make(chan time.Time)
			statCh      = make(chan autobuild.Stats)
			inactiveTTL = time.Minute
		)

		logger := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true}).Leveled(slog.LevelDebug)
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				AutobuildTicker:          ticker,
				IncludeProvisionerDaemon: true,
				AutobuildStats:           statCh,
				TemplateScheduleStore:    schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{wirtualsdk.FeatureAdvancedTemplateScheduling: 1},
			},
		})
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
			Parse:          echo.ParseComplete,
			ProvisionPlan:  echo.PlanComplete,
			ProvisionApply: echo.ApplyComplete,
		})
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.TimeTilDormantMillis = ptr.Ref[int64](inactiveTTL.Milliseconds())
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)

		ws := wirtualdtest.CreateWorkspace(t, client, template.ID, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.AutostartSchedule = nil
		})
		build := wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, ws.LatestBuild.ID)
		require.Equal(t, wirtualsdk.WorkspaceStatusRunning, build.Status)

		// Stop the workspace so we can assert autobuild does nothing
		// if we breach our inactivity threshold.
		ws = wirtualdtest.MustTransitionWorkspace(t, client, ws.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop)

		// Simulate not having accessed the workspace in a while.
		ticker <- ws.LastUsedAt.Add(2 * inactiveTTL)
		stats := <-statCh
		// Expect no transitions since workspace is stopped.
		require.Len(t, stats.Transitions, 0)
		ws = wirtualdtest.MustWorkspace(t, client, ws.ID)
		// The workspace should still be dormant even though we didn't
		// transition the workspace.
		require.NotNil(t, ws.DormantAt)
	})

	// Test the flow of a workspace transitioning from
	// inactive -> dormant -> deleted.
	t.Run("WorkspaceInactiveDeleteTransition", func(t *testing.T) {
		t.Parallel()

		var (
			ticker        = make(chan time.Time)
			statCh        = make(chan autobuild.Stats)
			transitionTTL = time.Minute
		)

		logger := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true}).Leveled(slog.LevelDebug)
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				AutobuildTicker:          ticker,
				IncludeProvisionerDaemon: true,
				AutobuildStats:           statCh,
				TemplateScheduleStore:    schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{wirtualsdk.FeatureAdvancedTemplateScheduling: 1},
			},
		})

		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
			Parse:          echo.ParseComplete,
			ProvisionPlan:  echo.PlanComplete,
			ProvisionApply: echo.ApplyComplete,
		})
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.TimeTilDormantMillis = ptr.Ref[int64](transitionTTL.Milliseconds())
			ctr.TimeTilDormantAutoDeleteMillis = ptr.Ref[int64](transitionTTL.Milliseconds())
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)

		ws := wirtualdtest.CreateWorkspace(t, client, template.ID)
		build := wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, ws.LatestBuild.ID)
		require.Equal(t, wirtualsdk.WorkspaceStatusRunning, build.Status)

		// Simulate not having accessed the workspace in a while.
		ticker <- ws.LastUsedAt.Add(2 * transitionTTL)
		stats := <-statCh
		// Expect workspace to transition to stopped state for breaching
		// inactive TTL.
		require.Len(t, stats.Transitions, 1)
		require.Equal(t, stats.Transitions[ws.ID], database.WorkspaceTransitionStop)

		ws = wirtualdtest.MustWorkspace(t, client, ws.ID)
		// The workspace should be dormant.
		require.NotNil(t, ws.DormantAt)

		// Wait for the autobuilder to stop the workspace.
		_ = wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, ws.LatestBuild.ID)

		// Simulate the workspace being dormant beyond the threshold.
		ticker <- ws.DormantAt.Add(2 * transitionTTL)
		stats = <-statCh
		require.Len(t, stats.Transitions, 1)
		// The workspace should be scheduled for deletion.
		require.Equal(t, stats.Transitions[ws.ID], database.WorkspaceTransitionDelete)

		// Wait for the workspace to be deleted.
		ws = wirtualdtest.MustWorkspace(t, client, ws.ID)
		_ = wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, ws.LatestBuild.ID)

		// Assert that the workspace is actually deleted.
		//nolint:gocritic // ensuring workspace is deleted and not just invisible to us due to RBAC
		_, err := client.Workspace(testutil.Context(t, testutil.WaitShort), ws.ID)
		require.Error(t, err)
		cerr, ok := wirtualsdk.AsError(err)
		require.True(t, ok)
		require.Equal(t, http.StatusGone, cerr.StatusCode())
	})

	t.Run("DormantTTLTooEarly", func(t *testing.T) {
		t.Parallel()

		var (
			ticker     = make(chan time.Time)
			statCh     = make(chan autobuild.Stats)
			dormantTTL = time.Minute
		)

		logger := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true}).Leveled(slog.LevelDebug)
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				AutobuildTicker:          ticker,
				IncludeProvisionerDaemon: true,
				AutobuildStats:           statCh,
				TemplateScheduleStore:    schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{wirtualsdk.FeatureAdvancedTemplateScheduling: 1},
			},
		})
		anotherClient, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
			Parse:          echo.ParseComplete,
			ProvisionPlan:  echo.PlanComplete,
			ProvisionApply: echo.ApplyComplete,
		})
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.TimeTilDormantAutoDeleteMillis = ptr.Ref[int64](dormantTTL.Milliseconds())
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		ws := wirtualdtest.CreateWorkspace(t, anotherClient, template.ID)
		build := wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, anotherClient, ws.LatestBuild.ID)
		require.Equal(t, wirtualsdk.WorkspaceStatusRunning, build.Status)

		ctx := testutil.Context(t, testutil.WaitMedium)
		err := anotherClient.UpdateWorkspaceDormancy(ctx, ws.ID, wirtualsdk.UpdateWorkspaceDormancy{
			Dormant: true,
		})
		require.NoError(t, err)

		ws = wirtualdtest.MustWorkspace(t, client, ws.ID)
		require.NotNil(t, ws.DormantAt)

		// Ensure we haven't breached our threshold.
		ticker <- ws.DormantAt.Add(-dormantTTL * 2)
		stats := <-statCh
		// Expect no transitions since not enough time has elapsed.
		require.Len(t, stats.Transitions, 0)

		_, err = anotherClient.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			TimeTilDormantAutoDeleteMillis: dormantTTL.Milliseconds(),
		})
		require.NoError(t, err)

		// Simlute the workspace breaching the threshold.
		ticker <- ws.DormantAt.Add(dormantTTL * 2)
		stats = <-statCh
		require.Len(t, stats.Transitions, 1)
		require.Equal(t, database.WorkspaceTransitionDelete, stats.Transitions[ws.ID])
	})

	// Assert that a dormant workspace does not autostart.
	t.Run("DormantNoAutostart", func(t *testing.T) {
		t.Parallel()

		var (
			ctx         = testutil.Context(t, testutil.WaitMedium)
			tickCh      = make(chan time.Time)
			statsCh     = make(chan autobuild.Stats)
			inactiveTTL = time.Minute
		)

		logger := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true}).Leveled(slog.LevelDebug)
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				AutobuildTicker:          tickCh,
				IncludeProvisionerDaemon: true,
				AutobuildStats:           statsCh,
				TemplateScheduleStore:    schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{wirtualsdk.FeatureAdvancedTemplateScheduling: 1},
			},
		})

		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
			Parse:          echo.ParseComplete,
			ProvisionPlan:  echo.PlanComplete,
			ProvisionApply: echo.ApplyComplete,
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)

		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		sched, err := cron.Weekly("CRON_TZ=UTC 0 * * * *")
		require.NoError(t, err)

		ws := wirtualdtest.CreateWorkspace(t, client, template.ID, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.AutostartSchedule = ptr.Ref(sched.String())
		})
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, ws.LatestBuild.ID)
		ws = wirtualdtest.MustTransitionWorkspace(t, client, ws.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop)

		// Assert that autostart works when the workspace isn't dormant..
		tickCh <- sched.Next(ws.LatestBuild.CreatedAt)
		stats := <-statsCh
		require.Len(t, stats.Errors, 0)
		require.Len(t, stats.Transitions, 1)
		require.Contains(t, stats.Transitions, ws.ID)
		require.Equal(t, database.WorkspaceTransitionStart, stats.Transitions[ws.ID])

		ws = wirtualdtest.MustWorkspace(t, client, ws.ID)
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, ws.LatestBuild.ID)

		// Now that we've validated that the workspace is eligible for autostart
		// lets cause it to become dormant.
		_, err = client.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			TimeTilDormantMillis: inactiveTTL.Milliseconds(),
		})
		require.NoError(t, err)

		// We should see the workspace get stopped now.
		tickCh <- ws.LastUsedAt.Add(inactiveTTL * 2)
		stats = <-statsCh
		require.Len(t, stats.Errors, 0)
		require.Len(t, stats.Transitions, 1)
		require.Contains(t, stats.Transitions, ws.ID)
		require.Equal(t, database.WorkspaceTransitionStop, stats.Transitions[ws.ID])

		// The workspace should be dormant now.
		ws = wirtualdtest.MustWorkspace(t, client, ws.ID)
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, ws.LatestBuild.ID)
		require.NotNil(t, ws.DormantAt)

		// Assert that autostart is no longer triggered since workspace is dormant.
		tickCh <- sched.Next(ws.LatestBuild.CreatedAt)
		stats = <-statsCh
		require.Len(t, stats.Transitions, 0)
	})

	// Test that failing to auto-delete a workspace will only retry
	// once a day.
	t.Run("FailedDeleteRetryDaily", func(t *testing.T) {
		t.Parallel()

		var (
			ticker        = make(chan time.Time)
			statCh        = make(chan autobuild.Stats)
			transitionTTL = time.Minute
			ctx           = testutil.Context(t, testutil.WaitMedium)
		)

		logger := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true}).Leveled(slog.LevelDebug)
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				AutobuildTicker:          ticker,
				IncludeProvisionerDaemon: true,
				AutobuildStats:           statCh,
				TemplateScheduleStore:    schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{wirtualsdk.FeatureAdvancedTemplateScheduling: 1},
			},
		})
		templateAdmin, _ := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleTemplateAdmin())

		// Create a template version that passes to get a functioning workspace.
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
			Parse:          echo.ParseComplete,
			ProvisionPlan:  echo.PlanComplete,
			ProvisionApply: echo.ApplyComplete,
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)

		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)

		ws := wirtualdtest.CreateWorkspace(t, templateAdmin, template.ID)
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, templateAdmin, ws.LatestBuild.ID)

		// Create a new version that will fail when we try to delete a workspace.
		version = wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, &echo.Responses{
			Parse:          echo.ParseComplete,
			ProvisionPlan:  echo.PlanComplete,
			ProvisionApply: echo.ApplyFailed,
		}, func(ctvr *wirtualsdk.CreateTemplateVersionRequest) {
			ctvr.TemplateID = template.ID
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)

		// Try to delete the workspace. This simulates a "failed" autodelete.
		build, err := templateAdmin.CreateWorkspaceBuild(ctx, ws.ID, wirtualsdk.CreateWorkspaceBuildRequest{
			Transition:        wirtualsdk.WorkspaceTransitionDelete,
			TemplateVersionID: version.ID,
		})
		require.NoError(t, err)

		build = wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, build.ID)
		require.NotEmpty(t, build.Job.Error)

		// Update our workspace to be dormant so that it qualifies for auto-deletion.
		err = templateAdmin.UpdateWorkspaceDormancy(ctx, ws.ID, wirtualsdk.UpdateWorkspaceDormancy{
			Dormant: true,
		})
		require.NoError(t, err)

		// Enable auto-deletion for the template.
		_, err = templateAdmin.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			TimeTilDormantAutoDeleteMillis: transitionTTL.Milliseconds(),
		})
		require.NoError(t, err)

		ws = wirtualdtest.MustWorkspace(t, client, ws.ID)
		require.NotNil(t, ws.DeletingAt)

		// Simulate ticking an hour after the workspace is expected to be deleted.
		// Under normal circumstances this should result in a transition but
		// since our last build resulted in failure it should be skipped.
		ticker <- build.Job.CompletedAt.Add(time.Hour)
		stats := <-statCh
		require.Len(t, stats.Transitions, 0)

		// Simulate ticking a day after the workspace was last attempted to
		// be deleted. This should result in an attempt.
		ticker <- build.Job.CompletedAt.Add(time.Hour * 25)
		stats = <-statCh
		require.Len(t, stats.Transitions, 1)
		require.Equal(t, database.WorkspaceTransitionDelete, stats.Transitions[ws.ID])
	})

	t.Run("RequireActiveVersion", func(t *testing.T) {
		t.Parallel()

		var (
			tickCh  = make(chan time.Time)
			statsCh = make(chan autobuild.Stats)
			ctx     = testutil.Context(t, testutil.WaitMedium)
		)

		logger := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true}).Leveled(slog.LevelDebug)
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				AutobuildTicker:          tickCh,
				IncludeProvisionerDaemon: true,
				AutobuildStats:           statsCh,
				TemplateScheduleStore:    schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{wirtualsdk.FeatureAccessControl: 1},
			},
		})

		sched, err := cron.Weekly("CRON_TZ=UTC 0 * * * *")
		require.NoError(t, err)

		// Create a template version1 that passes to get a functioning workspace.
		version1 := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version1.ID)

		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version1.ID)
		require.Equal(t, version1.ID, template.ActiveVersionID)

		ws := wirtualdtest.CreateWorkspace(t, client, template.ID, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.AutostartSchedule = ptr.Ref(sched.String())
		})

		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, ws.LatestBuild.ID)
		ws = wirtualdtest.MustTransitionWorkspace(t, client, ws.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop)

		// Create a new version so that we can assert we don't update
		// to the latest by default.
		version2 := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil, func(ctvr *wirtualsdk.CreateTemplateVersionRequest) {
			ctvr.TemplateID = template.ID
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version2.ID)

		// Make sure to promote it.
		err = client.UpdateActiveTemplateVersion(ctx, template.ID, wirtualsdk.UpdateActiveTemplateVersion{
			ID: version2.ID,
		})
		require.NoError(t, err)

		// Kick of an autostart build.
		tickCh <- sched.Next(ws.LatestBuild.CreatedAt)
		stats := <-statsCh
		require.Len(t, stats.Errors, 0)
		require.Len(t, stats.Transitions, 1)
		require.Contains(t, stats.Transitions, ws.ID)
		require.Equal(t, database.WorkspaceTransitionStart, stats.Transitions[ws.ID])

		// Validate that we didn't update to the promoted version.
		started := wirtualdtest.MustWorkspace(t, client, ws.ID)
		firstBuild := wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, started.LatestBuild.ID)
		require.Equal(t, version1.ID, firstBuild.TemplateVersionID)

		// Update the template to require the promoted version.
		_, err = client.UpdateTemplateMeta(ctx, template.ID, wirtualsdk.UpdateTemplateMeta{
			RequireActiveVersion: true,
			AllowUserAutostart:   true,
		})
		require.NoError(t, err)

		// Reset the workspace to the stopped state so we can try
		// to autostart again.
		wirtualdtest.MustTransitionWorkspace(t, client, ws.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop, func(req *wirtualsdk.CreateWorkspaceBuildRequest) {
			req.TemplateVersionID = ws.LatestBuild.TemplateVersionID
		})

		// Force an autostart transition again.
		tickCh <- sched.Next(firstBuild.CreatedAt)
		stats = <-statsCh
		require.Len(t, stats.Errors, 0)
		require.Len(t, stats.Transitions, 1)
		require.Contains(t, stats.Transitions, ws.ID)
		require.Equal(t, database.WorkspaceTransitionStart, stats.Transitions[ws.ID])

		// Validate that we are using the promoted version.
		ws = wirtualdtest.MustWorkspace(t, client, ws.ID)
		require.Equal(t, version2.ID, ws.LatestBuild.TemplateVersionID)
	})
}

func TestTemplateDoesNotAllowUserAutostop(t *testing.T) {
	t.Parallel()

	t.Run("TTLSetByTemplate", func(t *testing.T) {
		t.Parallel()
		logger := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true}).Leveled(slog.LevelDebug)
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			IncludeProvisionerDaemon: true,
			TemplateScheduleStore:    schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
		})
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		templateTTL := 24 * time.Hour.Milliseconds()
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.DefaultTTLMillis = ptr.Ref(templateTTL)
			ctr.AllowUserAutostop = ptr.Ref(false)
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		workspace := wirtualdtest.CreateWorkspace(t, client, template.ID, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.TTLMillis = nil // ensure that no default TTL is set
		})
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)

		// TTL should be set by the template
		require.Equal(t, false, template.AllowUserAutostop)
		require.Equal(t, templateTTL, template.DefaultTTLMillis)
		require.Equal(t, templateTTL, *workspace.TTLMillis)

		// Change the template's default TTL and refetch the workspace
		templateTTL = 72 * time.Hour.Milliseconds()
		ctx := testutil.Context(t, testutil.WaitShort)
		template = wirtualdtest.UpdateTemplateMeta(t, client, template.ID, wirtualsdk.UpdateTemplateMeta{
			DefaultTTLMillis: templateTTL,
		})
		workspace, err := client.Workspace(ctx, workspace.ID)
		require.NoError(t, err)

		// Ensure that the new value is reflected in the template and workspace
		require.Equal(t, templateTTL, template.DefaultTTLMillis)
		require.Equal(t, templateTTL, *workspace.TTLMillis)
	})

	t.Run("ExtendIsNotEnabledByTemplate", func(t *testing.T) {
		t.Parallel()
		logger := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true}).Leveled(slog.LevelDebug)
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			IncludeProvisionerDaemon: true,
			TemplateScheduleStore:    schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
		})
		user := wirtualdtest.CreateFirstUser(t, client)
		version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.AllowUserAutostop = ptr.Ref(false)
		})
		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		workspace := wirtualdtest.CreateWorkspace(t, client, template.ID)
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)

		require.Equal(t, false, template.AllowUserAutostop, "template should have AllowUserAutostop as false")

		ctx := testutil.Context(t, testutil.WaitShort)
		ttl := 8 * time.Hour
		newDeadline := time.Now().Add(ttl + time.Hour).UTC()

		err := client.PutExtendWorkspace(ctx, workspace.ID, wirtualsdk.PutExtendWorkspaceRequest{
			Deadline: newDeadline,
		})

		require.ErrorContains(t, err, "template does not allow user autostop")
	})
}

// Blocked by autostart requirements
func TestExecutorAutostartBlocked(t *testing.T) {
	t.Parallel()

	now := time.Now()
	var allowed []string
	for _, day := range agplschedule.DaysOfWeek {
		// Skip the day the workspace was created on and if the next day is within 2
		// hours, skip that too. The cron scheduler will start the workspace every hour,
		// so it can span into the next day.
		if day != now.UTC().Weekday() &&
			day != now.UTC().Add(time.Hour*2).Weekday() {
			allowed = append(allowed, day.String())
		}
	}

	var (
		sched   = must(cron.Weekly("CRON_TZ=UTC 0 * * * *"))
		tickCh  = make(chan time.Time)
		statsCh = make(chan autobuild.Stats)

		logger        = slogtest.Make(t, &slogtest.Options{IgnoreErrors: true}).Leveled(slog.LevelDebug)
		client, owner = wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				AutobuildTicker:          tickCh,
				IncludeProvisionerDaemon: true,
				AutobuildStats:           statsCh,
				TemplateScheduleStore:    schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{wirtualsdk.FeatureAdvancedTemplateScheduling: 1},
			},
		})
		version  = wirtualdtest.CreateTemplateVersion(t, client, owner.OrganizationID, nil)
		template = wirtualdtest.CreateTemplate(t, client, owner.OrganizationID, version.ID, func(request *wirtualsdk.CreateTemplateRequest) {
			request.AutostartRequirement = &wirtualsdk.TemplateAutostartRequirement{
				DaysOfWeek: allowed,
			}
		})
		_         = wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		workspace = wirtualdtest.CreateWorkspace(t, client, template.ID, func(cwr *wirtualsdk.CreateWorkspaceRequest) {
			cwr.AutostartSchedule = ptr.Ref(sched.String())
		})
		_ = wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)
	)

	// Given: workspace is stopped
	workspace = wirtualdtest.MustTransitionWorkspace(t, client, workspace.ID, database.WorkspaceTransitionStart, database.WorkspaceTransitionStop)

	// When: the autobuild executor ticks way into the future
	go func() {
		tickCh <- workspace.LatestBuild.CreatedAt.Add(24 * time.Hour)
		close(tickCh)
	}()

	// Then: the workspace should not be started.
	stats := <-statsCh
	require.Len(t, stats.Errors, 0)
	require.Len(t, stats.Transitions, 0)
}

func TestWorkspacesFiltering(t *testing.T) {
	t.Parallel()

	t.Run("Dormant", func(t *testing.T) {
		t.Parallel()

		ctx := testutil.Context(t, testutil.WaitMedium)
		logger := slogtest.Make(t, &slogtest.Options{IgnoreErrors: true}).Leveled(slog.LevelDebug)
		client, db, owner := wirtualdenttest.NewWithDatabase(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				TemplateScheduleStore: schedule.NewEnterpriseTemplateScheduleStore(agplUserQuietHoursScheduleStore(), notifications.NewNoopEnqueuer(), logger),
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{wirtualsdk.FeatureAdvancedTemplateScheduling: 1},
			},
		})
		templateAdminClient, templateAdmin := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID, rbac.RoleTemplateAdmin())

		resp := dbfake.TemplateVersion(t, db).Seed(database.TemplateVersion{
			OrganizationID: owner.OrganizationID,
			CreatedBy:      owner.UserID,
		}).Do()

		dormantWS1 := dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
			OwnerID:        templateAdmin.ID,
			OrganizationID: owner.OrganizationID,
		}).Do().Workspace

		dormantWS2 := dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
			OwnerID:        templateAdmin.ID,
			OrganizationID: owner.OrganizationID,
			TemplateID:     resp.Template.ID,
		}).Do().Workspace

		_ = dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
			OwnerID:        templateAdmin.ID,
			OrganizationID: owner.OrganizationID,
			TemplateID:     resp.Template.ID,
		}).Do().Workspace

		err := templateAdminClient.UpdateWorkspaceDormancy(ctx, dormantWS1.ID, wirtualsdk.UpdateWorkspaceDormancy{Dormant: true})
		require.NoError(t, err)

		err = templateAdminClient.UpdateWorkspaceDormancy(ctx, dormantWS2.ID, wirtualsdk.UpdateWorkspaceDormancy{Dormant: true})
		require.NoError(t, err)

		workspaces, err := templateAdminClient.Workspaces(ctx, wirtualsdk.WorkspaceFilter{
			FilterQuery: "dormant:true",
		})
		require.NoError(t, err)
		require.Len(t, workspaces.Workspaces, 2)

		for _, ws := range workspaces.Workspaces {
			if ws.ID != dormantWS1.ID && ws.ID != dormantWS2.ID {
				t.Fatalf("Unexpected workspace %+v", ws)
			}
		}
	})
}

// TestWorkspacesWithoutTemplatePerms creates a workspace for a user, then drops
// the user's perms to the underlying template.
func TestWorkspacesWithoutTemplatePerms(t *testing.T) {
	t.Parallel()

	client, first := wirtualdenttest.New(t, &wirtualdenttest.Options{
		Options: &wirtualdtest.Options{
			IncludeProvisionerDaemon: true,
		},
		LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureTemplateRBAC: 1,
			},
		},
	})

	version := wirtualdtest.CreateTemplateVersion(t, client, first.OrganizationID, nil)
	wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
	template := wirtualdtest.CreateTemplate(t, client, first.OrganizationID, version.ID)

	user, _ := wirtualdtest.CreateAnotherUser(t, client, first.OrganizationID)
	workspace := wirtualdtest.CreateWorkspace(t, user, template.ID)

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()

	// Remove everyone access
	//nolint:gocritic // creating a separate user just for this is overkill
	err := client.UpdateTemplateACL(ctx, template.ID, wirtualsdk.UpdateTemplateACL{
		GroupPerms: map[string]wirtualsdk.TemplateRole{
			first.OrganizationID.String(): wirtualsdk.TemplateRoleDeleted,
		},
	})
	require.NoError(t, err, "remove everyone access")

	// This should fail as the user cannot read the template
	_, err = user.Workspace(ctx, workspace.ID)
	require.Error(t, err, "fetch workspace")
	var sdkError *wirtualsdk.Error
	require.ErrorAs(t, err, &sdkError)
	require.Equal(t, http.StatusForbidden, sdkError.StatusCode())

	_, err = user.Workspaces(ctx, wirtualsdk.WorkspaceFilter{})
	require.NoError(t, err, "fetch workspaces should not fail")

	// Now create another workspace the user can read.
	version2 := wirtualdtest.CreateTemplateVersion(t, client, first.OrganizationID, nil)
	wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version2.ID)
	template2 := wirtualdtest.CreateTemplate(t, client, first.OrganizationID, version2.ID)
	_ = wirtualdtest.CreateWorkspace(t, user, template2.ID)

	workspaces, err := user.Workspaces(ctx, wirtualsdk.WorkspaceFilter{})
	require.NoError(t, err, "fetch workspaces should not fail")
	require.Len(t, workspaces.Workspaces, 1)
}

func TestWorkspaceLock(t *testing.T) {
	t.Parallel()

	t.Run("TemplateTimeTilDormantAutoDelete", func(t *testing.T) {
		t.Parallel()
		var (
			client, user = wirtualdenttest.New(t, &wirtualdenttest.Options{
				Options: &wirtualdtest.Options{
					IncludeProvisionerDaemon: true,
					TemplateScheduleStore:    &schedule.EnterpriseTemplateScheduleStore{},
				},
				LicenseOptions: &wirtualdenttest.LicenseOptions{
					Features: license.Features{
						wirtualsdk.FeatureAdvancedTemplateScheduling: 1,
					},
				},
			})

			version    = wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
			_          = wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
			dormantTTL = time.Minute
		)

		template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID, func(ctr *wirtualsdk.CreateTemplateRequest) {
			ctr.TimeTilDormantAutoDeleteMillis = ptr.Ref[int64](dormantTTL.Milliseconds())
		})

		workspace := wirtualdtest.CreateWorkspace(t, client, template.ID)
		_ = wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		lastUsedAt := workspace.LastUsedAt
		err := client.UpdateWorkspaceDormancy(ctx, workspace.ID, wirtualsdk.UpdateWorkspaceDormancy{
			Dormant: true,
		})
		require.NoError(t, err)

		workspace = wirtualdtest.MustWorkspace(t, client, workspace.ID)
		require.NoError(t, err, "fetch provisioned workspace")
		require.NotNil(t, workspace.DeletingAt)
		require.NotNil(t, workspace.DormantAt)
		require.Equal(t, workspace.DormantAt.Add(dormantTTL), *workspace.DeletingAt)
		require.WithinRange(t, *workspace.DormantAt, time.Now().Add(-time.Second*10), time.Now())
		// Locking a workspace shouldn't update the last_used_at.
		require.Equal(t, lastUsedAt, workspace.LastUsedAt)

		workspace = wirtualdtest.MustWorkspace(t, client, workspace.ID)
		lastUsedAt = workspace.LastUsedAt
		err = client.UpdateWorkspaceDormancy(ctx, workspace.ID, wirtualsdk.UpdateWorkspaceDormancy{
			Dormant: false,
		})
		require.NoError(t, err)

		workspace, err = client.Workspace(ctx, workspace.ID)
		require.NoError(t, err, "fetch provisioned workspace")
		require.Nil(t, workspace.DormantAt)
		// Unlocking a workspace should cause the deleting_at to be unset.
		require.Nil(t, workspace.DeletingAt)
		// The last_used_at should get updated when we unlock the workspace.
		require.True(t, workspace.LastUsedAt.After(lastUsedAt))
	})
}

func TestResolveAutostart(t *testing.T) {
	t.Parallel()

	ownerClient, db, owner := wirtualdenttest.NewWithDatabase(t, &wirtualdenttest.Options{
		Options: &wirtualdtest.Options{
			TemplateScheduleStore: &schedule.EnterpriseTemplateScheduleStore{},
		},
		LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureAccessControl: 1,
			},
		},
	})

	version1 := dbfake.TemplateVersion(t, db).
		Seed(database.TemplateVersion{
			CreatedBy:      owner.UserID,
			OrganizationID: owner.OrganizationID,
		}).Do()

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()

	_, err := ownerClient.UpdateTemplateMeta(ctx, version1.Template.ID, wirtualsdk.UpdateTemplateMeta{
		RequireActiveVersion: true,
	})
	require.NoError(t, err)

	client, member := wirtualdtest.CreateAnotherUser(t, ownerClient, owner.OrganizationID)

	workspace := dbfake.WorkspaceBuild(t, db, database.WorkspaceTable{
		OwnerID:        member.ID,
		OrganizationID: owner.OrganizationID,
		TemplateID:     version1.Template.ID,
	}).Seed(database.WorkspaceBuild{
		TemplateVersionID: version1.TemplateVersion.ID,
	}).Do().Workspace

	_ = dbfake.TemplateVersion(t, db).Seed(database.TemplateVersion{
		CreatedBy:      owner.UserID,
		OrganizationID: owner.OrganizationID,
		TemplateID:     version1.TemplateVersion.TemplateID,
	}).Params(database.TemplateVersionParameter{
		Name:     "param",
		Required: true,
	}).Do()

	// Autostart shouldn't be possible if parameters do not match.
	resp, err := client.ResolveAutostart(ctx, workspace.ID.String())
	require.NoError(t, err)
	require.True(t, resp.ParameterMismatch)
}

func TestAdminViewAllWorkspaces(t *testing.T) {
	t.Parallel()

	client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
		Options: &wirtualdtest.Options{
			IncludeProvisionerDaemon: true,
		},
		LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureMultipleOrganizations:      1,
				wirtualsdk.FeatureExternalProvisionerDaemons: 1,
			},
		},
	})

	version := wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
	wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
	template := wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
	workspace := wirtualdtest.CreateWorkspace(t, client, template.ID)
	wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()

	//nolint:gocritic // intentionally using owner
	_, err := client.Workspace(ctx, workspace.ID)
	require.NoError(t, err)

	otherOrg, err := client.CreateOrganization(ctx, wirtualsdk.CreateOrganizationRequest{
		Name: "default-test",
	})
	require.NoError(t, err, "create other org")

	// This other user is not in the first user's org. Since other is an admin, they can
	// still see the "first" user's workspace.
	otherOwner, _ := wirtualdtest.CreateAnotherUser(t, client, otherOrg.ID, rbac.RoleOwner())
	otherWorkspaces, err := otherOwner.Workspaces(ctx, wirtualsdk.WorkspaceFilter{})
	require.NoError(t, err, "(other) fetch workspaces")

	firstWorkspaces, err := client.Workspaces(ctx, wirtualsdk.WorkspaceFilter{})
	require.NoError(t, err, "(first) fetch workspaces")

	require.ElementsMatch(t, otherWorkspaces.Workspaces, firstWorkspaces.Workspaces)
	require.Equal(t, len(firstWorkspaces.Workspaces), 1, "should be 1 workspace present")

	memberView, _ := wirtualdtest.CreateAnotherUser(t, client, otherOrg.ID)
	memberViewWorkspaces, err := memberView.Workspaces(ctx, wirtualsdk.WorkspaceFilter{})
	require.NoError(t, err, "(member) fetch workspaces")
	require.Equal(t, 0, len(memberViewWorkspaces.Workspaces), "member in other org should see 0 workspaces")
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}
