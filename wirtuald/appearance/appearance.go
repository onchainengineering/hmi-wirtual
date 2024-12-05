package appearance

import (
	"context"

	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

type Fetcher interface {
	Fetch(ctx context.Context) (wirtualsdk.AppearanceConfig, error)
}

type AGPLFetcher struct {
	docsURL string
}

func (f AGPLFetcher) Fetch(context.Context) (wirtualsdk.AppearanceConfig, error) {
	return wirtualsdk.AppearanceConfig{
		AnnouncementBanners: []wirtualsdk.BannerConfig{},
		SupportLinks:        wirtualsdk.DefaultSupportLinks(f.docsURL),
		DocsURL:             f.docsURL,
	}, nil
}

func NewDefaultFetcher(docsURL string) Fetcher {
	if docsURL == "" {
		docsURL = wirtualsdk.DefaultDocsURL()
	}
	return &AGPLFetcher{
		docsURL: docsURL,
	}
}
