package main

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/safing/portbase/info"
	"github.com/spf13/cobra"
)

var showShortVersion bool
var showAllVersions bool
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Display various portmaster versions",
	Args:  cobra.NoArgs,
	PersistentPreRunE: func(*cobra.Command, []string) error {
		if showAllVersions {
			// if we are going to show all component versions
			// we need the dataroot to be configured.
			if err := configureDataRoot(); err != nil {
				return err
			}
		}

		return nil
	},
	RunE: func(*cobra.Command, []string) error {
		if !showAllVersions {
			if showShortVersion {
				fmt.Println(info.Version())
			}

			fmt.Println(info.FullVersion())
			return nil
		}

		fmt.Printf("portmaster-start %s\n\n", info.Version())
		fmt.Printf("Components:\n")

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
		tw.Flush()

		return nil
	},
}

func init() {
	flags := versionCmd.Flags()
	{
		flags.BoolVar(&showShortVersion, "short", false, "Print only the verison number.")
		flags.BoolVar(&showAllVersions, "all", false, "Dump versions for all components.")
	}

	rootCmd.AddCommand(versionCmd)
}
