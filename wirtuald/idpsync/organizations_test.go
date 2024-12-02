package idpsync_test

import (
	"testing"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"cdr.dev/slog/sloggers/slogtest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/idpsync"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/runtimeconfig"
	"github.com/onchainengineering/hmi-wirtual/testutil"
)

func TestParseOrganizationClaims(t *testing.T) {
	t.Parallel()

	t.Run("AGPL", func(t *testing.T) {
		t.Parallel()

		// AGPL has limited behavior
		s := idpsync.NewAGPLSync(slogtest.Make(t, &slogtest.Options{}),
			runtimeconfig.NewManager(),
			idpsync.DeploymentSyncSettings{
				OrganizationField: "orgs",
				OrganizationMapping: map[string][]uuid.UUID{
					"random": {uuid.New()},
				},
				OrganizationAssignDefault: false,
			})

		ctx := testutil.Context(t, testutil.WaitMedium)

		params, err := s.ParseOrganizationClaims(ctx, jwt.MapClaims{})
		require.Nil(t, err)

		require.False(t, params.SyncEntitled)
	})
}
