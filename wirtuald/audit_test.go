package wirtuald_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"cdr.dev/slog/sloggers/slogtest"
	"github.com/coder/coder/v2/wirtuald/audit"
	"github.com/coder/coder/v2/wirtuald/database"
	"github.com/coder/coder/v2/wirtuald/rbac"
	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
	"github.com/coder/coder/v2/wirtualsdk"
)

func TestAuditLogs(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)

		err := client.CreateTestAuditLog(ctx, wirtualsdk.CreateTestAuditLogRequest{
			ResourceID: user.UserID,
		})
		require.NoError(t, err)

		alogs, err := client.AuditLogs(ctx, wirtualsdk.AuditLogsRequest{
			Pagination: wirtualsdk.Pagination{
				Limit: 1,
			},
		})
		require.NoError(t, err)

		require.Equal(t, int64(1), alogs.Count)
		require.Len(t, alogs.AuditLogs, 1)
	})

	t.Run("IncludeUser", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		client := wirtualdtest.New(t, nil)
		user := wirtualdtest.CreateFirstUser(t, client)
		client2, user2 := wirtualdtest.CreateAnotherUser(t, client, user.OrganizationID, rbac.RoleOwner())

		err := client2.CreateTestAuditLog(ctx, wirtualsdk.CreateTestAuditLogRequest{
			ResourceID: user2.ID,
		})
		require.NoError(t, err)

		alogs, err := client.AuditLogs(ctx, wirtualsdk.AuditLogsRequest{
			Pagination: wirtualsdk.Pagination{
				Limit: 1,
			},
		})
		require.NoError(t, err)
		require.Equal(t, int64(1), alogs.Count)
		require.Len(t, alogs.AuditLogs, 1)

		// Make sure the returned user is fully populated.
		foundUser, err := client.User(ctx, user2.ID.String())
		foundUser.OrganizationIDs = []uuid.UUID{} // Not included.
		require.NoError(t, err)
		require.Equal(t, foundUser, *alogs.AuditLogs[0].User)

		// Delete the user and try again.  This is a soft delete so nothing should
		// change.  If users are hard deleted we should get nil, but there is no way
		// to test this at the moment.
		err = client.DeleteUser(ctx, user2.ID)
		require.NoError(t, err)

		alogs, err = client.AuditLogs(ctx, wirtualsdk.AuditLogsRequest{
			Pagination: wirtualsdk.Pagination{
				Limit: 1,
			},
		})
		require.NoError(t, err)
		require.Equal(t, int64(1), alogs.Count)
		require.Len(t, alogs.AuditLogs, 1)

		foundUser, err = client.User(ctx, user2.ID.String())
		foundUser.OrganizationIDs = []uuid.UUID{} // Not included.
		require.NoError(t, err)
		require.Equal(t, foundUser, *alogs.AuditLogs[0].User)
	})

	t.Run("WorkspaceBuildAuditLink", func(t *testing.T) {
		t.Parallel()

		var (
			ctx      = context.Background()
			client   = wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
			user     = wirtualdtest.CreateFirstUser(t, client)
			version  = wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
			template = wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		)

		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		workspace := wirtualdtest.CreateWorkspace(t, client, template.ID)
		wirtualdtest.AwaitWorkspaceBuildJobCompleted(t, client, workspace.LatestBuild.ID)

		buildResourceInfo := audit.AdditionalFields{
			WorkspaceName: workspace.Name,
			BuildNumber:   strconv.FormatInt(int64(workspace.LatestBuild.BuildNumber), 10),
			BuildReason:   database.BuildReason(string(workspace.LatestBuild.Reason)),
		}

		wriBytes, err := json.Marshal(buildResourceInfo)
		require.NoError(t, err)

		err = client.CreateTestAuditLog(ctx, wirtualsdk.CreateTestAuditLogRequest{
			Action:           wirtualsdk.AuditActionStop,
			ResourceType:     wirtualsdk.ResourceTypeWorkspaceBuild,
			ResourceID:       workspace.LatestBuild.ID,
			AdditionalFields: wriBytes,
		})
		require.NoError(t, err)

		auditLogs, err := client.AuditLogs(ctx, wirtualsdk.AuditLogsRequest{
			Pagination: wirtualsdk.Pagination{
				Limit: 1,
			},
		})
		require.NoError(t, err)
		buildNumberString := strconv.FormatInt(int64(workspace.LatestBuild.BuildNumber), 10)
		require.Equal(t, auditLogs.AuditLogs[0].ResourceLink, fmt.Sprintf("/@%s/%s/builds/%s",
			workspace.OwnerName, workspace.Name, buildNumberString))
	})

	t.Run("Organization", func(t *testing.T) {
		t.Parallel()

		logger := slogtest.Make(t, &slogtest.Options{
			IgnoreErrors: true,
		})
		ctx := context.Background()
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			Logger: &logger,
		})
		owner := wirtualdtest.CreateFirstUser(t, client)
		orgAdmin, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID, rbac.ScopedRoleOrgAdmin(owner.OrganizationID))

		err := client.CreateTestAuditLog(ctx, wirtualsdk.CreateTestAuditLogRequest{
			ResourceID:     owner.UserID,
			OrganizationID: owner.OrganizationID,
		})
		require.NoError(t, err)

		// Add an extra audit log in another organization
		err = client.CreateTestAuditLog(ctx, wirtualsdk.CreateTestAuditLogRequest{
			ResourceID: owner.UserID,
		})
		require.NoError(t, err)

		// Fetching audit logs without an organization selector should only
		// return organization audit logs the org admin is an admin of.
		alogs, err := orgAdmin.AuditLogs(ctx, wirtualsdk.AuditLogsRequest{
			Pagination: wirtualsdk.Pagination{
				Limit: 5,
			},
		})
		require.NoError(t, err)
		require.Len(t, alogs.AuditLogs, 1)

		// Using the organization selector allows the org admin to fetch audit logs
		alogs, err = orgAdmin.AuditLogs(ctx, wirtualsdk.AuditLogsRequest{
			SearchQuery: fmt.Sprintf("organization:%s", owner.OrganizationID.String()),
			Pagination: wirtualsdk.Pagination{
				Limit: 5,
			},
		})
		require.NoError(t, err)
		require.Len(t, alogs.AuditLogs, 1)

		// Also try fetching by organization name
		organization, err := orgAdmin.Organization(ctx, owner.OrganizationID)
		require.NoError(t, err)

		alogs, err = orgAdmin.AuditLogs(ctx, wirtualsdk.AuditLogsRequest{
			SearchQuery: fmt.Sprintf("organization:%s", organization.Name),
			Pagination: wirtualsdk.Pagination{
				Limit: 5,
			},
		})
		require.NoError(t, err)
		require.Len(t, alogs.AuditLogs, 1)
	})

	t.Run("Organization404", func(t *testing.T) {
		t.Parallel()

		logger := slogtest.Make(t, &slogtest.Options{
			IgnoreErrors: true,
		})
		ctx := context.Background()
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			Logger: &logger,
		})
		owner := wirtualdtest.CreateFirstUser(t, client)
		orgAdmin, _ := wirtualdtest.CreateAnotherUser(t, client, owner.OrganizationID, rbac.ScopedRoleOrgAdmin(owner.OrganizationID))

		_, err := orgAdmin.AuditLogs(ctx, wirtualsdk.AuditLogsRequest{
			SearchQuery: fmt.Sprintf("organization:%s", "random-name"),
			Pagination: wirtualsdk.Pagination{
				Limit: 5,
			},
		})
		require.Error(t, err)
	})
}

func TestAuditLogsFilter(t *testing.T) {
	t.Parallel()

	t.Run("Filter", func(t *testing.T) {
		t.Parallel()

		var (
			ctx      = context.Background()
			client   = wirtualdtest.New(t, &wirtualdtest.Options{IncludeProvisionerDaemon: true})
			user     = wirtualdtest.CreateFirstUser(t, client)
			version  = wirtualdtest.CreateTemplateVersion(t, client, user.OrganizationID, nil)
			template = wirtualdtest.CreateTemplate(t, client, user.OrganizationID, version.ID)
		)

		wirtualdtest.AwaitTemplateVersionJobCompleted(t, client, version.ID)
		workspace := wirtualdtest.CreateWorkspace(t, client, template.ID)

		// Create two logs with "Create"
		err := client.CreateTestAuditLog(ctx, wirtualsdk.CreateTestAuditLogRequest{
			Action:       wirtualsdk.AuditActionCreate,
			ResourceType: wirtualsdk.ResourceTypeTemplate,
			ResourceID:   template.ID,
			Time:         time.Date(2022, 8, 15, 14, 30, 45, 100, time.UTC), // 2022-8-15 14:30:45
		})
		require.NoError(t, err)
		err = client.CreateTestAuditLog(ctx, wirtualsdk.CreateTestAuditLogRequest{
			Action:       wirtualsdk.AuditActionCreate,
			ResourceType: wirtualsdk.ResourceTypeUser,
			ResourceID:   user.UserID,
			Time:         time.Date(2022, 8, 16, 14, 30, 45, 100, time.UTC), // 2022-8-16 14:30:45
		})
		require.NoError(t, err)

		// Create one log with "Delete"
		err = client.CreateTestAuditLog(ctx, wirtualsdk.CreateTestAuditLogRequest{
			Action:       wirtualsdk.AuditActionDelete,
			ResourceType: wirtualsdk.ResourceTypeUser,
			ResourceID:   user.UserID,
			Time:         time.Date(2022, 8, 15, 14, 30, 45, 100, time.UTC), // 2022-8-15 14:30:45
		})
		require.NoError(t, err)

		// Create one log with "Start"
		err = client.CreateTestAuditLog(ctx, wirtualsdk.CreateTestAuditLogRequest{
			Action:       wirtualsdk.AuditActionStart,
			ResourceType: wirtualsdk.ResourceTypeWorkspaceBuild,
			ResourceID:   workspace.LatestBuild.ID,
			Time:         time.Date(2022, 8, 15, 14, 30, 45, 100, time.UTC), // 2022-8-15 14:30:45
		})
		require.NoError(t, err)

		// Create one log with "Stop"
		err = client.CreateTestAuditLog(ctx, wirtualsdk.CreateTestAuditLogRequest{
			Action:       wirtualsdk.AuditActionStop,
			ResourceType: wirtualsdk.ResourceTypeWorkspaceBuild,
			ResourceID:   workspace.LatestBuild.ID,
			Time:         time.Date(2022, 8, 15, 14, 30, 45, 100, time.UTC), // 2022-8-15 14:30:45
		})
		require.NoError(t, err)

		// Test cases
		testCases := []struct {
			Name           string
			SearchQuery    string
			ExpectedResult int
			ExpectedError  bool
		}{
			{
				Name:           "FilterByCreateAction",
				SearchQuery:    "action:create",
				ExpectedResult: 2,
			},
			{
				Name:           "FilterByDeleteAction",
				SearchQuery:    "action:delete",
				ExpectedResult: 1,
			},
			{
				Name:           "FilterByUserResourceType",
				SearchQuery:    "resource_type:user",
				ExpectedResult: 2,
			},
			{
				Name:           "FilterByTemplateResourceType",
				SearchQuery:    "resource_type:template",
				ExpectedResult: 1,
			},
			{
				Name:           "FilterByEmail",
				SearchQuery:    "email:" + wirtualdtest.FirstUserParams.Email,
				ExpectedResult: 5,
			},
			{
				Name:           "FilterByUsername",
				SearchQuery:    "username:" + wirtualdtest.FirstUserParams.Username,
				ExpectedResult: 5,
			},
			{
				Name:           "FilterByResourceID",
				SearchQuery:    "resource_id:" + user.UserID.String(),
				ExpectedResult: 2,
			},
			{
				Name:          "FilterInvalidSingleValue",
				SearchQuery:   "invalid",
				ExpectedError: true,
			},
			{
				Name:          "FilterWithInvalidResourceType",
				SearchQuery:   "resource_type:invalid",
				ExpectedError: true,
			},
			{
				Name:          "FilterWithInvalidAction",
				SearchQuery:   "action:invalid",
				ExpectedError: true,
			},
			{
				Name:           "FilterOnCreateSingleDay",
				SearchQuery:    "action:create date_from:2022-08-15 date_to:2022-08-15",
				ExpectedResult: 1,
			},
			{
				Name:           "FilterOnCreateDateFrom",
				SearchQuery:    "action:create date_from:2022-08-15",
				ExpectedResult: 2,
			},
			{
				Name:           "FilterOnCreateDateTo",
				SearchQuery:    "action:create date_to:2022-08-15",
				ExpectedResult: 1,
			},
			{
				Name:           "FilterOnWorkspaceBuildStart",
				SearchQuery:    "resource_type:workspace_build action:start",
				ExpectedResult: 1,
			},
			{
				Name:           "FilterOnWorkspaceBuildStop",
				SearchQuery:    "resource_type:workspace_build action:stop",
				ExpectedResult: 1,
			},
			{
				Name:           "FilterOnWorkspaceBuildStartByInitiator",
				SearchQuery:    "resource_type:workspace_build action:start build_reason:initiator",
				ExpectedResult: 1,
			},
		}

		for _, testCase := range testCases {
			testCase := testCase
			// Test filtering
			t.Run(testCase.Name, func(t *testing.T) {
				t.Parallel()
				auditLogs, err := client.AuditLogs(ctx, wirtualsdk.AuditLogsRequest{
					SearchQuery: testCase.SearchQuery,
				})
				if testCase.ExpectedError {
					require.Error(t, err, "expected error")
				} else {
					require.NoError(t, err, "fetch audit logs")
					require.Len(t, auditLogs.AuditLogs, testCase.ExpectedResult, "expected audit logs returned")
					require.Equal(t, testCase.ExpectedResult, int(auditLogs.Count), "expected audit log count returned")
				}
			})
		}
	})
}
