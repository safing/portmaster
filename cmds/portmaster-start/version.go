package main

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/safing/portmaster/base/info"
)

var (
	showShortVersion bool
	showAllVersions  bool
	versionCmd       = &cobra.Command{
		Use:   "version",
		Short: "Display various portmaster versions",
		Args:  cobra.NoArgs,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			if showAllVersions {
				// If we are going to show all component versions,
				// we need the registry to be configured.
				if err := configureRegistry(false); err != nil {
					return err
				}
			}

			return nil
		},
		RunE: func(*cobra.Command, []string) error {
			if !showAllVersions {
				if showShortVersion {
					fmt.Println(info.Version())
					return nil
				}

				fmt.Println(info.FullVersion())
				return nil
			}

			fmt.Printf("portmaster-start %s\n\n", info.Version())
			fmt.Printf("Assets:\n")

			all := registry.Export()
			keys := make([]string, 0, len(all))
			for identifier := range all {
				keys = append(keys, identifier)
			}
			sort.Strings(keys)

			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			for _, identifier := range keys {
				res := all[identifier]

				if showShortVersion {
					// in "short" mode, skip all resources that are irrelevant on that platform
					if !strings.HasPrefix(identifier, "all") && !strings.HasPrefix(identifier, runtime.GOOS) {
						continue
					}
				}

				fmt.Fprintf(tw, "   %s\t%s\n", identifier, res.SelectedVersion.VersionNumber)
			}
			return tw.Flush()
		},
	}
)

func init() {
	flags := versionCmd.Flags()
	{
		flags.BoolVar(&showShortVersion, "short", false, "Print only the version number.")
		flags.BoolVar(&showAllVersions, "all", false, "Dump versions for all assets.")
	}

	rootCmd.AddCommand(versionCmd)
}
