package wirtuald_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbtestutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func TestRegions(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()
		const appHostname = "*.apps.coder.test"

		db, pubsub := dbtestutil.NewDB(t)
		deploymentID := uuid.New()

		ctx := testutil.Context(t, testutil.WaitLong)
		err := db.InsertDeploymentID(ctx, deploymentID.String())
		require.NoError(t, err)

		client := wirtualdtest.New(t, &wirtualdtest.Options{
			AppHostname: appHostname,
			Database:    db,
			Pubsub:      pubsub,
		})
		_ = wirtualdtest.CreateFirstUser(t, client)

		regions, err := client.Regions(ctx)
		require.NoError(t, err)

		require.Len(t, regions, 1)
		require.NotEqual(t, uuid.Nil, regions[0].ID)
		require.Equal(t, regions[0].ID, deploymentID)
		require.Equal(t, "primary", regions[0].Name)
		require.Equal(t, "Default", regions[0].DisplayName)
		require.NotEmpty(t, regions[0].IconURL)
		require.True(t, regions[0].Healthy)
		require.Equal(t, client.URL.String(), regions[0].PathAppURL)
		require.Equal(t, fmt.Sprintf("%s:%s", appHostname, client.URL.Port()), regions[0].WildcardHostname)

		// Ensure the primary region ID is constant.
		regions2, err := client.Regions(ctx)
		require.NoError(t, err)
		require.Equal(t, regions[0].ID, regions2[0].ID)
	})

	t.Run("RequireAuth", func(t *testing.T) {
		t.Parallel()

		ctx := testutil.Context(t, testutil.WaitLong)
		client := wirtualdtest.New(t, nil)
		_ = wirtualdtest.CreateFirstUser(t, client)

		unauthedClient := wirtualsdk.New(client.URL)
		regions, err := unauthedClient.Regions(ctx)
		require.Error(t, err)
		require.Empty(t, regions)
	})
}
