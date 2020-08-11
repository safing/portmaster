package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/safing/portmaster/firewall/interception"
	"github.com/spf13/cobra"
)

var recoverIPTablesCmd = &cobra.Command{
	Use:   "recover-iptables",
	Short: "Removes obsolete IP tables rules in case of an unclean shutdown",
	RunE: func(*cobra.Command, []string) error {
		// interception.DeactiveNfqueueFirewall uses coreos/go-iptables
		// which shells out to the /sbin/iptables binary. As a result,
		// we don't get the errno of the actual error and need to parse the
		// output instead. Make sure it's always english by setting LC_ALL=C
		currentLocale := os.Getenv("LC_ALL")
		os.Setenv("LC_ALL", "C")                 // nolint:errcheck - we tried at least ...
		defer os.Setenv("LC_ALL", currentLocale) // nolint:errcheck

		err := interception.DeactivateNfqueueFirewall()
		if err == nil {
			return nil
		}

		// we don't want to show ErrNotExists to the user
		// as that only means portmaster did the cleanup itself.
		mr, ok := err.(*multierror.Error)
		if !ok {
			return err
		}

		var filteredErrors *multierror.Error
		for _, err := range mr.Errors {
			// if we have a permission denied error, all errors will be the same
			if strings.Contains(err.Error(), "Permission denied") {
				return fmt.Errorf("failed to cleanup iptables: %w", os.ErrPermission)
			}

			if !strings.Contains(err.Error(), "No such file or directory") {
				filteredErrors = multierror.Append(filteredErrors, err)
			}
		}

		if filteredErrors != nil {
			filteredErrors.ErrorFormat = formatNfqErrors
			return filteredErrors
		}

		return nil
	},
	SilenceUsage: true,
}

func init() {
	rootCmd.AddCommand(recoverIPTablesCmd)
}

func formatNfqErrors(es []error) string {
	if len(es) == 1 {
		return fmt.Sprintf("1 error occurred:\n\t* %s\n\n", es[0])
	}

	points := make([]string, len(es))
	for i, err := range es {
		// only display the very first line of each error
		first := strings.Split(err.Error(), "\n")[0]
		points[i] = fmt.Sprintf("* %s", first)
	}

	return fmt.Sprintf(
		"%d errors occurred:\n\t%s\n\n",
		len(es), strings.Join(points, "\n\t"))
}
