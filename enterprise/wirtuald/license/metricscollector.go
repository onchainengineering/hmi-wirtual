package license

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/onchainengineering/hmi-wirtual/wirtuald/entitlements"
	"github.com/onchainengineering/hmi-wirtual/wirtualsdk"
)

var (
	activeUsersDesc      = prometheus.NewDesc("wirtuald_license_active_users", "The number of active users.", nil, nil)
	limitUsersDesc       = prometheus.NewDesc("wirtuald_license_limit_users", "The user seats limit based on the active Coder license.", nil, nil)
	userLimitEnabledDesc = prometheus.NewDesc("wirtuald_license_user_limit_enabled", "Returns 1 if the current license enforces the user limit.", nil, nil)
)

type MetricsCollector struct {
	Entitlements *entitlements.Set
}

var _ prometheus.Collector = new(MetricsCollector)

func (*MetricsCollector) Describe(descCh chan<- *prometheus.Desc) {
	descCh <- activeUsersDesc
	descCh <- limitUsersDesc
	descCh <- userLimitEnabledDesc
}

func (mc *MetricsCollector) Collect(metricsCh chan<- prometheus.Metric) {
	userLimitEntitlement, ok := mc.Entitlements.Feature(wirtualsdk.FeatureUserLimit)
	if !ok {
		return
	}

	var enabled float64
	if userLimitEntitlement.Enabled {
		enabled = 1
	}
	metricsCh <- prometheus.MustNewConstMetric(userLimitEnabledDesc, prometheus.GaugeValue, enabled)

	if userLimitEntitlement.Actual != nil {
		metricsCh <- prometheus.MustNewConstMetric(activeUsersDesc, prometheus.GaugeValue, float64(*userLimitEntitlement.Actual))
	}

	if userLimitEntitlement.Limit != nil {
		metricsCh <- prometheus.MustNewConstMetric(limitUsersDesc, prometheus.GaugeValue, float64(*userLimitEntitlement.Limit))
	}
}
