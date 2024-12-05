package enidpsync_test

import (
	"testing"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"

	"cdr.dev/slog/sloggers/slogtest"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/enidpsync"
	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/entitlements"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/idpsync"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/runtimeconfig"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestEnterpriseParseGroupClaims(t *testing.T) {
	t.Parallel()

	entitled := entitlements.New()
	entitled.Modify(func(entitlements *wirtualsdk.Entitlements) {
		entitlements.Features[wirtualsdk.FeatureTemplateRBAC] = wirtualsdk.Feature{
			Entitlement: wirtualsdk.EntitlementEntitled,
			Enabled:     true,
		}
	})

	t.Run("NoEntitlements", func(t *testing.T) {
		t.Parallel()

		s := enidpsync.NewSync(slogtest.Make(t, &slogtest.Options{}),
			runtimeconfig.NewManager(),
			entitlements.New(),
			idpsync.DeploymentSyncSettings{})

		ctx := testutil.Context(t, testutil.WaitMedium)

		params, err := s.ParseGroupClaims(ctx, jwt.MapClaims{})
		require.Nil(t, err)

		require.False(t, params.SyncEntitled)
	})

	t.Run("NotInAllowList", func(t *testing.T) {
		t.Parallel()

		s := enidpsync.NewSync(slogtest.Make(t, &slogtest.Options{}),
			runtimeconfig.NewManager(),
			entitled,
			idpsync.DeploymentSyncSettings{
				GroupField: "groups",
				GroupAllowList: map[string]struct{}{
					"foo": {},
				},
			})

		ctx := testutil.Context(t, testutil.WaitMedium)

		// Try with incorrect group
		_, err := s.ParseGroupClaims(ctx, jwt.MapClaims{
			"groups": []string{"bar"},
		})
		require.NotNil(t, err)
		require.Equal(t, 403, err.Code)

		// Try with no groups
		_, err = s.ParseGroupClaims(ctx, jwt.MapClaims{})
		require.NotNil(t, err)
		require.Equal(t, 403, err.Code)
	})

	t.Run("InAllowList", func(t *testing.T) {
		t.Parallel()

		s := enidpsync.NewSync(slogtest.Make(t, &slogtest.Options{}),
			runtimeconfig.NewManager(),
			entitled,
			idpsync.DeploymentSyncSettings{
				GroupField: "groups",
				GroupAllowList: map[string]struct{}{
					"foo": {},
				},
			})

		ctx := testutil.Context(t, testutil.WaitMedium)

		claims := jwt.MapClaims{
			"groups": []string{"foo", "bar"},
		}
		params, err := s.ParseGroupClaims(ctx, claims)
		require.Nil(t, err)
		require.True(t, params.SyncEntitled)
		require.Equal(t, claims, params.MergedClaims)
	})
}
