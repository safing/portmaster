package metrics

import (
	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/log"
)

func registerLogMetrics() (err error) {
	_, err = NewFetchingCounter(
		"logs/warning/total",
		nil,
		log.TotalWarningLogLines,
		&Options{
			Name:       "Total Warning Log Lines",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	_, err = NewFetchingCounter(
		"logs/error/total",
		nil,
		log.TotalErrorLogLines,
		&Options{
			Name:       "Total Error Log Lines",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	_, err = NewFetchingCounter(
		"logs/critical/total",
		nil,
		log.TotalCriticalLogLines,
		&Options{
			Name:       "Total Critical Log Lines",
			Permission: api.PermitUser,
		},
	)
	if err != nil {
		return err
	}

	return nil
}
