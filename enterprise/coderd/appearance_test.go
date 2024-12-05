package wirtuald_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/agent/proto"
	"github.com/coder/coder/v2/enterprise/wirtuald/license"
	"github.com/coder/coder/v2/enterprise/wirtuald/wirtualdenttest"
	"github.com/coder/coder/v2/testutil"
	"github.com/coder/coder/v2/wirtuald/database"
	"github.com/coder/coder/v2/wirtuald/database/dbfake"
	"github.com/coder/coder/v2/wirtuald/database/dbtestutil"
	"github.com/coder/coder/v2/wirtuald/wirtualdtest"
	"github.com/coder/coder/v2/wirtualsdk"
	"github.com/coder/coder/v2/wirtualsdk/agentsdk"
	"github.com/coder/serpent"
)

func TestCustomLogoAndCompanyName(t *testing.T) {
	t.Parallel()

	// Prepare enterprise deployment
	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
	defer cancel()

	adminClient, adminUser := wirtualdenttest.New(t, &wirtualdenttest.Options{DontAddLicense: true})
	wirtualdenttest.AddLicense(t, adminClient, wirtualdenttest.LicenseOptions{
		Features: license.Features{
			wirtualsdk.FeatureAppearance: 1,
		},
	})

	anotherClient, _ := wirtualdtest.CreateAnotherUser(t, adminClient, adminUser.OrganizationID)

	// Update logo and application name
	uac := wirtualsdk.UpdateAppearanceConfig{
		ApplicationName: "ACME Ltd",
		LogoURL:         "http://logo-url/file.png",
	}

	err := adminClient.UpdateAppearance(ctx, uac)
	require.NoError(t, err)

	// Verify update
	got, err := anotherClient.Appearance(ctx)
	require.NoError(t, err)

	require.Equal(t, uac.ApplicationName, got.ApplicationName)
	require.Equal(t, uac.LogoURL, got.LogoURL)
}

func TestAnnouncementBanners(t *testing.T) {
	t.Parallel()

	t.Run("User", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		adminClient, adminUser := wirtualdenttest.New(t, &wirtualdenttest.Options{DontAddLicense: true})
		basicUserClient, _ := wirtualdtest.CreateAnotherUser(t, adminClient, adminUser.OrganizationID)

		// Without a license, there should be no banners.
		sb, err := basicUserClient.Appearance(ctx)
		require.NoError(t, err)
		require.Empty(t, sb.AnnouncementBanners)

		wirtualdenttest.AddLicense(t, adminClient, wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureAppearance: 1,
			},
		})

		// Default state
		sb, err = basicUserClient.Appearance(ctx)
		require.NoError(t, err)
		require.Empty(t, sb.AnnouncementBanners)

		// Regular user should be unable to set the banner
		uac := wirtualsdk.UpdateAppearanceConfig{
			AnnouncementBanners: []wirtualsdk.BannerConfig{{Enabled: true}},
		}
		err = basicUserClient.UpdateAppearance(ctx, uac)
		require.Error(t, err)
		var sdkError *wirtualsdk.Error
		require.True(t, errors.As(err, &sdkError))
		require.ErrorAs(t, err, &sdkError)
		require.Equal(t, http.StatusForbidden, sdkError.StatusCode())

		// But an admin can
		wantBanner := wirtualsdk.UpdateAppearanceConfig{
			AnnouncementBanners: []wirtualsdk.BannerConfig{{
				Enabled:         true,
				Message:         "The beep-bop will be boop-beeped on Saturday at 12AM PST.",
				BackgroundColor: "#00FF00",
			}},
		}
		err = adminClient.UpdateAppearance(ctx, wantBanner)
		require.NoError(t, err)
		gotBanner, err := adminClient.Appearance(ctx) //nolint:gocritic // we should assert at least once that the owner can get the banner
		require.NoError(t, err)
		require.Equal(t, wantBanner.AnnouncementBanners, gotBanner.AnnouncementBanners)

		// But even an admin can't give a bad color
		wantBanner.AnnouncementBanners[0].BackgroundColor = "#bad color"
		err = adminClient.UpdateAppearance(ctx, wantBanner)
		require.Error(t, err)
		var sdkErr *wirtualsdk.Error
		require.ErrorAs(t, err, &sdkErr)
		require.Equal(t, http.StatusBadRequest, sdkErr.StatusCode())
		require.Contains(t, sdkErr.Message, "Invalid color format")
		require.Contains(t, sdkErr.Detail, "expected # prefix and 6 characters")
	})

	t.Run("Agent", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitLong)
		defer cancel()

		store, ps := dbtestutil.NewDB(t)
		client, user := wirtualdenttest.New(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				Database: store,
				Pubsub:   ps,
			},
			DontAddLicense: true,
		})
		lic := wirtualdenttest.AddLicense(t, client, wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureAppearance: 1,
			},
		})
		cfg := wirtualsdk.UpdateAppearanceConfig{
			AnnouncementBanners: []wirtualsdk.BannerConfig{{
				Enabled:         true,
				Message:         "The beep-bop will be boop-beeped on Saturday at 12AM PST.",
				BackgroundColor: "#00FF00",
			}},
		}
		err := client.UpdateAppearance(ctx, cfg)
		require.NoError(t, err)

		r := dbfake.WorkspaceBuild(t, store, database.WorkspaceTable{
			OrganizationID: user.OrganizationID,
			OwnerID:        user.UserID,
		}).WithAgent().Do()

		agentClient := agentsdk.New(client.URL)
		agentClient.SetSessionToken(r.AgentToken)
		banners := requireGetAnnouncementBanners(ctx, t, agentClient)
		require.Equal(t, cfg.AnnouncementBanners, banners)

		// Create an AGPL Wirtuald against the same database
		agplClient := wirtualdtest.New(t, &wirtualdtest.Options{Database: store, Pubsub: ps})
		agplAgentClient := agentsdk.New(agplClient.URL)
		agplAgentClient.SetSessionToken(r.AgentToken)
		banners = requireGetAnnouncementBanners(ctx, t, agplAgentClient)
		require.Equal(t, []wirtualsdk.BannerConfig{}, banners)

		// No license means no banner.
		err = client.DeleteLicense(ctx, lic.ID)
		require.NoError(t, err)
		banners = requireGetAnnouncementBanners(ctx, t, agentClient)
		require.Equal(t, []wirtualsdk.BannerConfig{}, banners)
	})
}

func requireGetAnnouncementBanners(ctx context.Context, t *testing.T, client *agentsdk.Client) []wirtualsdk.BannerConfig {
	cc, err := client.ConnectRPC(ctx)
	require.NoError(t, err)
	defer func() {
		_ = cc.Close()
	}()
	aAPI := proto.NewDRPCAgentClient(cc)
	bannersProto, err := aAPI.GetAnnouncementBanners(ctx, &proto.GetAnnouncementBannersRequest{})
	require.NoError(t, err)
	banners := make([]wirtualsdk.BannerConfig, 0, len(bannersProto.AnnouncementBanners))
	for _, bannerProto := range bannersProto.AnnouncementBanners {
		banners = append(banners, agentsdk.BannerConfigFromProto(bannerProto))
	}
	return banners
}

func TestCustomSupportLinks(t *testing.T) {
	t.Parallel()

	supportLinks := []wirtualsdk.LinkConfig{
		{
			Name:   "First link",
			Target: "http://first-link-1",
			Icon:   "chat",
		},
		{
			Name:   "Second link",
			Target: "http://second-link-2",
			Icon:   "bug",
		},
	}
	cfg := wirtualdtest.DeploymentValues(t)
	cfg.Support.Links = serpent.Struct[[]wirtualsdk.LinkConfig]{
		Value: supportLinks,
	}

	adminClient, adminUser := wirtualdenttest.New(t, &wirtualdenttest.Options{
		Options: &wirtualdtest.Options{
			DeploymentValues: cfg,
		},
		LicenseOptions: &wirtualdenttest.LicenseOptions{
			Features: license.Features{
				wirtualsdk.FeatureAppearance: 1,
			},
		},
	})

	anotherClient, _ := wirtualdtest.CreateAnotherUser(t, adminClient, adminUser.OrganizationID)
	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitMedium)
	defer cancel()

	appr, err := anotherClient.Appearance(ctx)
	require.NoError(t, err)
	require.Equal(t, supportLinks, appr.SupportLinks)
}

func TestCustomDocsURL(t *testing.T) {
	t.Parallel()

	testURLRawString := "http://google.com"
	testURL, err := url.Parse(testURLRawString)
	require.NoError(t, err)
	cfg := wirtualdtest.DeploymentValues(t)
	cfg.DocsURL = *serpent.URLOf(testURL)
	adminClient, adminUser := wirtualdenttest.New(t, &wirtualdenttest.Options{DontAddLicense: true, Options: &wirtualdtest.Options{DeploymentValues: cfg}})
	anotherClient, _ := wirtualdtest.CreateAnotherUser(t, adminClient, adminUser.OrganizationID)

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitMedium)
	defer cancel()

	appr, err := anotherClient.Appearance(ctx)
	require.NoError(t, err)
	require.Equal(t, testURLRawString, appr.DocsURL)
}

func TestDefaultSupportLinksWithCustomDocsUrl(t *testing.T) {
	t.Parallel()

	// Don't need to set the license, as default links are passed without it.
	testURLRawString := "http://google.com"
	testURL, err := url.Parse(testURLRawString)
	require.NoError(t, err)
	cfg := wirtualdtest.DeploymentValues(t)
	cfg.DocsURL = *serpent.URLOf(testURL)
	adminClient, adminUser := wirtualdenttest.New(t, &wirtualdenttest.Options{DontAddLicense: true, Options: &wirtualdtest.Options{DeploymentValues: cfg}})
	anotherClient, _ := wirtualdtest.CreateAnotherUser(t, adminClient, adminUser.OrganizationID)

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitMedium)
	defer cancel()

	appr, err := anotherClient.Appearance(ctx)
	require.NoError(t, err)
	require.Equal(t, wirtualsdk.DefaultSupportLinks(testURLRawString), appr.SupportLinks)
}

func TestDefaultSupportLinks(t *testing.T) {
	t.Parallel()

	// Don't need to set the license, as default links are passed without it.
	adminClient, adminUser := wirtualdenttest.New(t, &wirtualdenttest.Options{DontAddLicense: true})
	anotherClient, _ := wirtualdtest.CreateAnotherUser(t, adminClient, adminUser.OrganizationID)

	ctx, cancel := context.WithTimeout(context.Background(), testutil.WaitMedium)
	defer cancel()

	appr, err := anotherClient.Appearance(ctx)
	require.NoError(t, err)
	require.Equal(t, wirtualsdk.DefaultSupportLinks(wirtualsdk.DefaultDocsURL()), appr.SupportLinks)
}
