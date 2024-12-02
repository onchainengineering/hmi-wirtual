package agentapi

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/xerrors"

	agentproto "github.com/coder/coder/v2/agent/proto"
	"github.com/coder/coder/v2/wirtuald/appearance"
	"github.com/coder/coder/v2/wirtualsdk"
	"github.com/coder/coder/v2/wirtualsdk/agentsdk"
)

func TestGetAnnouncementBanners(t *testing.T) {
	t.Parallel()

	t.Run("OK", func(t *testing.T) {
		t.Parallel()

		cfg := []wirtualsdk.BannerConfig{{
			Enabled:         true,
			Message:         "The beep-bop will be boop-beeped on Saturday at 12AM PST.",
			BackgroundColor: "#00FF00",
		}}

		var ff appearance.Fetcher = fakeFetcher{cfg: wirtualsdk.AppearanceConfig{AnnouncementBanners: cfg}}
		ptr := atomic.Pointer[appearance.Fetcher]{}
		ptr.Store(&ff)

		api := &AnnouncementBannerAPI{appearanceFetcher: &ptr}
		resp, err := api.GetAnnouncementBanners(context.Background(), &agentproto.GetAnnouncementBannersRequest{})
		require.NoError(t, err)
		require.Len(t, resp.AnnouncementBanners, 1)
		require.Equal(t, cfg[0], agentsdk.BannerConfigFromProto(resp.AnnouncementBanners[0]))
	})

	t.Run("FetchError", func(t *testing.T) {
		t.Parallel()

		expectedErr := xerrors.New("badness")
		var ff appearance.Fetcher = fakeFetcher{err: expectedErr}
		ptr := atomic.Pointer[appearance.Fetcher]{}
		ptr.Store(&ff)

		api := &AnnouncementBannerAPI{appearanceFetcher: &ptr}
		resp, err := api.GetAnnouncementBanners(context.Background(), &agentproto.GetAnnouncementBannersRequest{})
		require.Error(t, err)
		require.ErrorIs(t, err, expectedErr)
		require.Nil(t, resp)
	})
}

type fakeFetcher struct {
	cfg wirtualsdk.AppearanceConfig
	err error
}

func (f fakeFetcher) Fetch(context.Context) (wirtualsdk.AppearanceConfig, error) {
	return f.cfg, f.err
}
