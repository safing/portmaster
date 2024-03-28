package nameserver

import (
	"github.com/safing/portbase/api"
	"github.com/safing/portbase/config"
	"github.com/safing/portbase/metrics"
)

var (
	requestsHistogram    *metrics.Histogram
	totalHandledRequests *metrics.Counter
)

func registerMetrics() (err error) {
	requestsHistogram, err = metrics.NewHistogram(
		"nameserver/request/duration/seconds",
		nil,
		&metrics.Options{
			Permission:     api.PermitUser,
			ExpertiseLevel: config.ExpertiseLevelExpert,
		},
	)
	if err != nil {
		return err
	}

	totalHandledRequests, err = metrics.NewCounter(
		"nameserver/request/total",
		nil,
		&metrics.Options{
			InternalID:     "handled_dns_requests",
			Permission:     api.PermitUser,
			ExpertiseLevel: config.ExpertiseLevelExpert,
			Persist:        true,
		},
	)
	if err != nil {
		return err
	}

	return nil
}
