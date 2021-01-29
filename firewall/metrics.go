package firewall

import (
	"github.com/safing/portbase/api"
	"github.com/safing/portbase/config"
	"github.com/safing/portbase/metrics"
)

var packetHandlingHistogram *metrics.Histogram

func registerMetrics() (err error) {
	packetHandlingHistogram, err = metrics.NewHistogram(
		"firewall/handling/duration/seconds",
		nil,
		&metrics.Options{
			Permission:     api.PermitUser,
			ExpertiseLevel: config.ExpertiseLevelExpert,
		})

	return err
}
