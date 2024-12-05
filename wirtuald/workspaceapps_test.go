package wirtuald_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/testutil"
	"github.com/coder/coder/v2/wirtuald/cryptokeys"
	"github.com/coder/coder/v2/wirtuald/database"
	"github.com/coder/coder/v2/wirtuald/database/dbgen"
	"github.com/coder/coder/v2/wirtuald/database/dbtestutil"
	"github.com/coder/coder/v2/wirtuald/jwtutils"
	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
	"github.com/coder/coder/v2/wirtuald/workspaceapps"
	"github.com/coder/coder/v2/wirtualsdk"
	"github.com/coder/quartz"
)

func TestGetAppHost(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		accessURL   string
		appHostname string
		expected    string
	}{
		{
			name:        "OK",
			accessURL:   "https://test.coder.com",
			appHostname: "*.test.coder.com",
			expected:    "*.test.coder.com",
		},
		{
			name:        "None",
			accessURL:   "https://test.coder.com",
			appHostname: "",
			expected:    "",
		},
		{
			name:        "OKWithPort",
			accessURL:   "https://test.coder.com:8443",
			appHostname: "*.test.coder.com",
			expected:    "*.test.coder.com:8443",
		},
		{
			name:        "OKWithSuffix",
			accessURL:   "https://test.coder.com:8443",
			appHostname: "*--suffix.test.coder.com",
			expected:    "*--suffix.test.coder.com:8443",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			accessURL, err := url.Parse(c.accessURL)
			require.NoError(t, err)

			client := wirtualdtest.New(t, &wirtualdtest.Options{
				AccessURL:   accessURL,
				AppHostname: c.appHostname,
			})

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			// Should not leak to unauthenticated users.
			host, err := client.AppHost(ctx)
			require.Error(t, err)
			require.Equal(t, "", host.Host)

			_ = wirtualdtest.CreateFirstUser(t, client)
			host, err = client.AppHost(ctx)
			require.NoError(t, err)
			require.Equal(t, c.expected, host.Host)
		})
	}
}

func TestWorkspaceApplicationAuth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		accessURL        string
		appHostname      string
		proxyURL         string
		proxyAppHostname string

		redirectURI    string
		expectRedirect string
	}{
		{
			name:             "OK",
			accessURL:        "https://test.coder.com",
			appHostname:      "*.test.coder.com",
			proxyURL:         "https://proxy.test.coder.com",
			proxyAppHostname: "*.proxy.test.coder.com",
			redirectURI:      "https://something.test.coder.com",
			expectRedirect:   "https://something.test.coder.com",
		},
		{
			name:             "ProxyPathOK",
			accessURL:        "https://test.coder.com",
			appHostname:      "*.test.coder.com",
			proxyURL:         "https://proxy.test.coder.com",
			proxyAppHostname: "*.proxy.test.coder.com",
			redirectURI:      "https://proxy.test.coder.com/path",
			expectRedirect:   "https://proxy.test.coder.com/path",
		},
		{
			name:             "ProxySubdomainOK",
			accessURL:        "https://test.coder.com",
			appHostname:      "*.test.coder.com",
			proxyURL:         "https://proxy.test.coder.com",
			proxyAppHostname: "*.proxy.test.coder.com",
			redirectURI:      "https://something.proxy.test.coder.com/path?yeah=true",
			expectRedirect:   "https://something.proxy.test.coder.com/path?yeah=true",
		},
		{
			name:             "ProxySubdomainSuffixOK",
			accessURL:        "https://test.coder.com",
			appHostname:      "*.test.coder.com",
			proxyURL:         "https://proxy.test.coder.com",
			proxyAppHostname: "*--suffix.proxy.test.coder.com",
			redirectURI:      "https://something--suffix.proxy.test.coder.com/",
			expectRedirect:   "https://something--suffix.proxy.test.coder.com/",
		},
		{
			name:             "NormalizeSchemePrimaryAppHostname",
			accessURL:        "https://test.coder.com",
			appHostname:      "*.test.coder.com",
			proxyURL:         "https://proxy.test.coder.com",
			proxyAppHostname: "*.proxy.test.coder.com",
			redirectURI:      "http://x.test.coder.com",
			expectRedirect:   "https://x.test.coder.com",
		},
		{
			name:             "NormalizeSchemeProxyAppHostname",
			accessURL:        "https://test.coder.com",
			appHostname:      "*.test.coder.com",
			proxyURL:         "https://proxy.test.coder.com",
			proxyAppHostname: "*.proxy.test.coder.com",
			redirectURI:      "http://x.proxy.test.coder.com",
			expectRedirect:   "https://x.proxy.test.coder.com",
		},
		{
			name:             "NoneError",
			accessURL:        "https://test.coder.com",
			appHostname:      "*.test.coder.com",
			proxyURL:         "https://proxy.test.coder.com",
			proxyAppHostname: "*.proxy.test.coder.com",
			redirectURI:      "",
			expectRedirect:   "",
		},
		{
			name:             "PrimaryAccessURLError",
			accessURL:        "https://test.coder.com",
			appHostname:      "*.test.coder.com",
			proxyURL:         "https://proxy.test.coder.com",
			proxyAppHostname: "*.proxy.test.coder.com",
			redirectURI:      "https://test.coder.com/",
			expectRedirect:   "",
		},
		{
			name:             "OtherError",
			accessURL:        "https://test.coder.com",
			appHostname:      "*.test.coder.com",
			proxyURL:         "https://proxy.test.coder.com",
			proxyAppHostname: "*.proxy.test.coder.com",
			redirectURI:      "https://example.com/",
			expectRedirect:   "",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			ctx := testutil.Context(t, testutil.WaitMedium)
			logger := testutil.Logger(t)
			accessURL, err := url.Parse(c.accessURL)
			require.NoError(t, err)

			db, ps := dbtestutil.NewDB(t)
			fetcher := &cryptokeys.DBFetcher{
				DB: db,
			}

			kc, err := cryptokeys.NewEncryptionCache(ctx, logger, fetcher, wirtualsdk.CryptoKeyFeatureWorkspaceAppsAPIKey)
			require.NoError(t, err)

			clock := quartz.NewMock(t)

			client := wirtualdtest.New(t, &wirtualdtest.Options{
				AccessURL:             accessURL,
				AppHostname:           c.appHostname,
				Database:              db,
				Pubsub:                ps,
				APIKeyEncryptionCache: kc,
				Clock:                 clock,
			})
			_ = wirtualdtest.CreateFirstUser(t, client)

			// Disable redirects.
			client.HTTPClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			}

			_, _ = dbgen.WorkspaceProxy(t, db, database.WorkspaceProxy{
				Url:              c.proxyURL,
				WildcardHostname: c.proxyAppHostname,
			})

			ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
			defer cancel()

			resp, err := client.Request(ctx, http.MethodGet, "/api/v2/applications/auth-redirect", nil, func(req *http.Request) {
				q := req.URL.Query()
				q.Set("redirect_uri", c.redirectURI)
				req.URL.RawQuery = q.Encode()
			})
			require.NoError(t, err)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusSeeOther {
				err = wirtualsdk.ReadBodyAsError(resp)
				if c.expectRedirect == "" {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				return
			}
			if c.expectRedirect == "" {
				t.Fatal("expected a failure but got a success")
			}

			loc, err := resp.Location()
			require.NoError(t, err)
			q := loc.Query()

			// Verify the API key is set.
			encryptedAPIKey := loc.Query().Get(workspaceapps.SubdomainProxyAPIKeyParam)
			require.NotEmpty(t, encryptedAPIKey, "no API key was set in the query parameters")

			// Strip the API key from the actual redirect URI and compare.
			q.Del(workspaceapps.SubdomainProxyAPIKeyParam)
			loc.RawQuery = q.Encode()
			require.Equal(t, c.expectRedirect, loc.String())

			var token workspaceapps.EncryptedAPIKeyPayload
			err = jwtutils.Decrypt(ctx, kc, encryptedAPIKey, &token, jwtutils.WithDecryptExpected(jwt.Expected{
				Time:        clock.Now(),
				AnyAudience: jwt.Audience{"wsproxy"},
				Issuer:      "wirtuald",
			}))
			require.NoError(t, err)
			require.Equal(t, jwt.NewNumericDate(clock.Now().Add(time.Minute)), token.Expiry)
			require.Equal(t, jwt.NewNumericDate(clock.Now().Add(-time.Minute)), token.NotBefore)
		})
	}
}
