package main

import (
	"log"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
)

var healthCheckReportURL string

func init() {
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&healthCheckReportURL, "report-to-healthcheck", "", "report to the given healthchecks URL")
}

func reportToHealthCheckIfEnabled(_ *cobra.Command, failureErr error) {
	if healthCheckReportURL == "" {
		return
	}

	if failureErr != nil {
		// Report failure.
		resp, err := http.Post(
			healthCheckReportURL+"/fail",
			"text/plain; utf-8",
			strings.NewReader(failureErr.Error()),
		)
		if err != nil {
			log.Printf("failed to report failure to healthcheck at %q: %s", healthCheckReportURL, err)
			return
		}
		_ = resp.Body.Close()

		// Always log that we've report the error.
		log.Printf("reported failure to healthcheck at %q", healthCheckReportURL)
	} else {
		// Report success.
		resp, err := http.Get(healthCheckReportURL) //nolint:gosec
		if err != nil {
			log.Printf("failed to report success to healthcheck at %q: %s", healthCheckReportURL, err)
			return
		}
		_ = resp.Body.Close()

		if verbose {
			log.Printf("reported success to healthcheck at %q", healthCheckReportURL)
		}
	}
}
