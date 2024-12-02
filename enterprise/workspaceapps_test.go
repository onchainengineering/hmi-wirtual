package enterprise_test

import (
	"net"
	"testing"

	"github.com/onchainengineering/hmi-wirtual/wirtuald/wirtualdtest"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/database/dbtestutil"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/httpmw"
	"github.com/onchainengineering/hmi-wirtual/wirtuald/workspaceapps/apptest"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/wirtualdenttest"
	"github.com/onchainengineering/hmi-wirtual/enterprise/wirtuald/license"
	"github.com/coder/serpent"
)

func TestWorkspaceApps(t *testing.T) {
	t.Parallel()

	apptest.Run(t, true, func(t *testing.T, opts *apptest.DeploymentOptions) *apptest.Deployment {
		deploymentValues := wirtualdtest.DeploymentValues(t)
		deploymentValues.DisablePathApps = serpent.Bool(opts.DisablePathApps)
		deploymentValues.Dangerous.AllowPathAppSharing = serpent.Bool(opts.DangerousAllowPathAppSharing)
		deploymentValues.Dangerous.AllowPathAppSiteOwnerAccess = serpent.Bool(opts.DangerousAllowPathAppSiteOwnerAccess)
		deploymentValues.Experiments = []string{
			"*",
		}

		if opts.DisableSubdomainApps {
			opts.AppHost = ""
		}

		flushStatsCollectorCh := make(chan chan<- struct{}, 1)
		opts.StatsCollectorOptions.Flush = flushStatsCollectorCh
		flushStats := func() {
			flushStatsCollectorDone := make(chan struct{}, 1)
			flushStatsCollectorCh <- flushStatsCollectorDone
			<-flushStatsCollectorDone
		}

		db, pubsub := dbtestutil.NewDB(t)

		client, _, _, user := wirtualdenttest.NewWithAPI(t, &wirtualdenttest.Options{
			Options: &wirtualdtest.Options{
				DeploymentValues:         deploymentValues,
				AppHostname:              opts.AppHost,
				IncludeProvisionerDaemon: true,
				RealIPConfig: &httpmw.RealIPConfig{
					TrustedOrigins: []*net.IPNet{{
						IP:   net.ParseIP("127.0.0.1"),
						Mask: net.CIDRMask(8, 32),
					}},
					TrustedHeaders: []string{
						"CF-Connecting-IP",
					},
				},
				WorkspaceAppsStatsCollectorOptions: opts.StatsCollectorOptions,
				Database:                           db,
				Pubsub:                             pubsub,
			},
			LicenseOptions: &wirtualdenttest.LicenseOptions{
				Features: license.Features{
					wirtualsdk.FeatureMultipleOrganizations: 1,
				},
			},
		})

		return &apptest.Deployment{
			Options:        opts,
			SDKClient:      client,
			FirstUser:      user,
			PathAppBaseURL: client.URL,
			FlushStats:     flushStats,
		}
	})
}
