package cmdbase

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/safing/portmaster/base/info"
)

var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version and related metadata.",
	RunE:  Version,
}

func Version(cmd *cobra.Command, args []string) error {
	fmt.Println(info.FullVersion())
	return nil
}
