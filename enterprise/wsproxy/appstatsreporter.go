package wsproxy

import (
	"context"

	"github.com/coder/coder/v2/enterprise/wsproxy/wsproxysdk"
	"github.com/coder/coder/v2/wirtuald/workspaceapps"
)

var _ workspaceapps.StatsReporter = (*appStatsReporter)(nil)

type appStatsReporter struct {
	Client *wsproxysdk.Client
}

func (r *appStatsReporter) ReportAppStats(ctx context.Context, stats []workspaceapps.StatsReport) error {
	err := r.Client.ReportAppStats(ctx, wsproxysdk.ReportAppStatsRequest{
		Stats: stats,
	})
	return err
}
