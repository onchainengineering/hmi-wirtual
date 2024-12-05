package wirtuald_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/onchainengineering/hmi-wirtual/testutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/httpmw"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

func Test_Experiments(t *testing.T) {
	t.Parallel()
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		cfg := wirtualdtest.DeploymentValues(t)
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			DeploymentValues: cfg,
		})
		_ = wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		experiments, err := client.Experiments(ctx)
		require.NoError(t, err)
		require.NotNil(t, experiments)
		require.Empty(t, experiments)
		require.False(t, experiments.Enabled("foo"))
	})

	t.Run("multiple features", func(t *testing.T) {
		t.Parallel()
		cfg := wirtualdtest.DeploymentValues(t)
		cfg.Experiments = []string{"foo", "BAR"}
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			DeploymentValues: cfg,
		})
		_ = wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		experiments, err := client.Experiments(ctx)
		require.NoError(t, err)
		require.NotNil(t, experiments)
		// Should be lower-cased.
		require.ElementsMatch(t, []wirtualsdk.Experiment{"foo", "bar"}, experiments)
		require.True(t, experiments.Enabled("foo"))
		require.True(t, experiments.Enabled("bar"))
		require.False(t, experiments.Enabled("baz"))
	})

	t.Run("wildcard", func(t *testing.T) {
		t.Parallel()
		cfg := wirtualdtest.DeploymentValues(t)
		cfg.Experiments = []string{"*"}
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			DeploymentValues: cfg,
		})
		_ = wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		experiments, err := client.Experiments(ctx)
		require.NoError(t, err)
		require.NotNil(t, experiments)
		require.ElementsMatch(t, wirtualsdk.ExperimentsAll, experiments)
		for _, ex := range wirtualsdk.ExperimentsAll {
			require.True(t, experiments.Enabled(ex))
		}
		require.False(t, experiments.Enabled("danger"))
	})

	t.Run("alternate wildcard with manual opt-in", func(t *testing.T) {
		t.Parallel()
		cfg := wirtualdtest.DeploymentValues(t)
		cfg.Experiments = []string{"*", "dAnGeR"}
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			DeploymentValues: cfg,
		})
		_ = wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		experiments, err := client.Experiments(ctx)
		require.NoError(t, err)
		require.NotNil(t, experiments)
		require.ElementsMatch(t, append(wirtualsdk.ExperimentsAll, "danger"), experiments)
		for _, ex := range wirtualsdk.ExperimentsAll {
			require.True(t, experiments.Enabled(ex))
		}
		require.True(t, experiments.Enabled("danger"))
		require.False(t, experiments.Enabled("herebedragons"))
	})

	t.Run("Unauthorized", func(t *testing.T) {
		t.Parallel()
		cfg := wirtualdtest.DeploymentValues(t)
		cfg.Experiments = []string{"*"}
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			DeploymentValues: cfg,
		})
		// Explicitly omit creating a user so we're unauthorized.
		// _ = wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		_, err := client.Experiments(ctx)
		require.Error(t, err)
		require.ErrorContains(t, err, httpmw.SignedOutErrorMessage)
	})

	t.Run("available experiments", func(t *testing.T) {
		t.Parallel()
		cfg := wirtualdtest.DeploymentValues(t)
		client := wirtualdtest.New(t, &wirtualdtest.Options{
			DeploymentValues: cfg,
		})
		_ = wirtualdtest.CreateFirstUser(t, client)

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		experiments, err := client.SafeExperiments(ctx)
		require.NoError(t, err)
		require.NotNil(t, experiments)
		require.ElementsMatch(t, wirtualsdk.ExperimentsAll, experiments.Safe)
	})
}
