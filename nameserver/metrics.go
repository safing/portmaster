package nameserver

import (
	"github.com/safing/portbase/api"
	"github.com/safing/portbase/config"
	"github.com/safing/portbase/metrics"
)

var requestsHistogram *metrics.Histogram

func registerMetrics() (err error) {
	requestsHistogram, err = metrics.NewHistogram(
		"nameserver/request/duration/seconds",
		nil,
		&metrics.Options{
			Permission:     api.PermitUser,
			ExpertiseLevel: config.ExpertiseLevelExpert,
		})

	return err
}
