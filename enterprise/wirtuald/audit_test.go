package wirtuald_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestEnterpriseAuditLogs(t *testing.T) {
	t.Parallel()

	t.Run("IncludeOrganization", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureMultipleOrganizations: 1,
				},
			},
		})

		//nolint:gocritic // only owners can create organizations
		o, err := client.CreateOrganization(ctx, wirtualsdk.CreateOrganizationRequest{
			Name:        "new-org",
			DisplayName: "New organization",
			Description: "A new organization to love and cherish until the test is over.",
			Icon:        "/emojis/1f48f-1f3ff.png",
		})
		require.NoError(t, err)

		err = client.CreateTestAuditLog(ctx, wirtualsdk.CreateTestAuditLogRequest{
			OrganizationID: o.ID,
			ResourceID:     user.UserID,
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

		// Make sure the organization is fully populated.
		require.Equal(t, &wirtualsdk.MinimalOrganization{
			ID:          o.ID,
			Name:        o.Name,
			DisplayName: o.DisplayName,
			Icon:        o.Icon,
		}, alogs.AuditLogs[0].Organization)

		// OrganizationID is deprecated, but make sure it is set.
		require.Equal(t, o.ID, alogs.AuditLogs[0].OrganizationID)

		// Delete the org and try again, should be mostly empty.
		err = client.DeleteOrganization(ctx, o.ID.String())
		require.NoError(t, err)

		alogs, err = client.AuditLogs(ctx, wirtualsdk.AuditLogsRequest{
			Pagination: wirtualsdk.Pagination{
				Limit: 1,
			},
		})
		require.NoError(t, err)
		require.Equal(t, int64(1), alogs.Count)
		require.Len(t, alogs.AuditLogs, 1)

		require.Equal(t, &wirtualsdk.MinimalOrganization{
			ID: o.ID,
		}, alogs.AuditLogs[0].Organization)

		// OrganizationID is deprecated, but make sure it is set.
		require.Equal(t, o.ID, alogs.AuditLogs[0].OrganizationID)

		// Some audit entries do not have an organization at all, in which case the
		// response omits the organization.
		err = client.CreateTestAuditLog(ctx, wirtualsdk.CreateTestAuditLogRequest{
			ResourceType: wirtualsdk.ResourceTypeAPIKey,
			ResourceID:   user.UserID,
		})
		require.NoError(t, err)

		alogs, err = client.AuditLogs(ctx, wirtualsdk.AuditLogsRequest{
			SearchQuery: "resource_type:api_key",
			Pagination: wirtualsdk.Pagination{
				Limit: 1,
			},
		})
		require.NoError(t, err)
		require.Equal(t, int64(1), alogs.Count)
		require.Len(t, alogs.AuditLogs, 1)

		// The other will have no organization.
		require.Equal(t, (*wirtualsdk.MinimalOrganization)(nil), alogs.AuditLogs[0].Organization)

		// OrganizationID is deprecated, but make sure it is empty.
		require.Equal(t, uuid.Nil, alogs.AuditLogs[0].OrganizationID)
	})
}
