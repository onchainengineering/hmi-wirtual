package entitlements_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/testutil"
	"github.com/coder/coder/v2/wirtuald/entitlements"
	"github.com/coder/coder/v2/wirtualsdk"
)

func TestModify(t *testing.T) {
	t.Parallel()

	set := entitlements.New()
	require.False(t, set.Enabled(wirtualsdk.FeatureMultipleOrganizations))

	set.Modify(func(entitlements *wirtualsdk.Entitlements) {
		entitlements.Features[wirtualsdk.FeatureMultipleOrganizations] = wirtualsdk.Feature{
			Enabled:     true,
			Entitlement: wirtualsdk.EntitlementEntitled,
		}
	})
	require.True(t, set.Enabled(wirtualsdk.FeatureMultipleOrganizations))
}

func TestAllowRefresh(t *testing.T) {
	t.Parallel()

	now := time.Now()
	set := entitlements.New()
	set.Modify(func(entitlements *wirtualsdk.Entitlements) {
		entitlements.RefreshedAt = now
	})

	ok, wait := set.AllowRefresh(now)
	require.False(t, ok)
	require.InDelta(t, time.Minute.Seconds(), wait.Seconds(), 5)

	set.Modify(func(entitlements *wirtualsdk.Entitlements) {
		entitlements.RefreshedAt = now.Add(time.Minute * -2)
	})

	ok, wait = set.AllowRefresh(now)
	require.True(t, ok)
	require.Equal(t, time.Duration(0), wait)
}

func TestUpdate(t *testing.T) {
	t.Parallel()
	ctx := testutil.Context(t, testutil.WaitShort)

	set := entitlements.New()
	require.False(t, set.Enabled(wirtualsdk.FeatureMultipleOrganizations))
	fetchStarted := make(chan struct{})
	firstDone := make(chan struct{})
	errCh := make(chan error, 2)
	go func() {
		err := set.Update(ctx, func(_ context.Context) (wirtualsdk.Entitlements, error) {
			close(fetchStarted)
			select {
			case <-firstDone:
				// OK!
			case <-ctx.Done():
				t.Error("timeout")
				return wirtualsdk.Entitlements{}, ctx.Err()
			}
			return wirtualsdk.Entitlements{
				Features: map[wirtualsdk.FeatureName]wirtualsdk.Feature{
					wirtualsdk.FeatureMultipleOrganizations: {
						Enabled: true,
					},
				},
			}, nil
		})
		errCh <- err
	}()
	testutil.RequireRecvCtx(ctx, t, fetchStarted)
	require.False(t, set.Enabled(wirtualsdk.FeatureMultipleOrganizations))
	// start a second update while the first one is in progress
	go func() {
		err := set.Update(ctx, func(_ context.Context) (wirtualsdk.Entitlements, error) {
			return wirtualsdk.Entitlements{
				Features: map[wirtualsdk.FeatureName]wirtualsdk.Feature{
					wirtualsdk.FeatureMultipleOrganizations: {
						Enabled: true,
					},
					wirtualsdk.FeatureAppearance: {
						Enabled: true,
					},
				},
			}, nil
		})
		errCh <- err
	}()
	close(firstDone)
	err := testutil.RequireRecvCtx(ctx, t, errCh)
	require.NoError(t, err)
	err = testutil.RequireRecvCtx(ctx, t, errCh)
	require.NoError(t, err)
	require.True(t, set.Enabled(wirtualsdk.FeatureMultipleOrganizations))
	require.True(t, set.Enabled(wirtualsdk.FeatureAppearance))
}

func TestUpdate_LicenseRequiresTelemetry(t *testing.T) {
	t.Parallel()
	ctx := testutil.Context(t, testutil.WaitShort)
	set := entitlements.New()
	set.Modify(func(entitlements *wirtualsdk.Entitlements) {
		entitlements.Errors = []string{"some error"}
		entitlements.Features[wirtualsdk.FeatureAppearance] = wirtualsdk.Feature{
			Enabled: true,
		}
	})
	err := set.Update(ctx, func(_ context.Context) (wirtualsdk.Entitlements, error) {
		return wirtualsdk.Entitlements{}, entitlements.ErrLicenseRequiresTelemetry
	})
	require.NoError(t, err)
	require.True(t, set.Enabled(wirtualsdk.FeatureAppearance))
	require.Equal(t, []string{entitlements.ErrLicenseRequiresTelemetry.Error()}, set.Errors())
}
