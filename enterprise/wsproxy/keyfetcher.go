package wsproxy

import (
	"context"

	"golang.org/x/xerrors"

	"github.com/onchainengineering/hmi-wirtual/enterprise/wsproxy/wsproxysdk"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/cryptokeys"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
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
