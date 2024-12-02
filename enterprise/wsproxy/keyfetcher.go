package wsproxy

import (
	"context"

	"golang.org/x/xerrors"

	"github.com/coder/coder/v2/wirtuald/cryptokeys"
	"github.com/coder/coder/v2/wirtualsdk"
	"github.com/coder/coder/v2/enterprise/wsproxy/wsproxysdk"
)

var _ cryptokeys.Fetcher = &ProxyFetcher{}

type ProxyFetcher struct {
	Client *wsproxysdk.Client
}

func (p *ProxyFetcher) Fetch(ctx context.Context, feature wirtualsdk.CryptoKeyFeature) ([]wirtualsdk.CryptoKey, error) {
	keys, err := p.Client.CryptoKeys(ctx, feature)
	if err != nil {
		return nil, xerrors.Errorf("crypto keys: %w", err)
	}
	return keys.CryptoKeys, nil
}
