package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/safing/portbase/info"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/updates"
	"github.com/spf13/cobra"
)

const (
	logPrefix = "[control]"
)

var (
	updateStoragePath string
	databaseRootDir   *string

	rootCmd = &cobra.Command{
		Use:               "portmaster-control",
		Short:             "contoller for all portmaster components",
		PersistentPreRunE: initPmCtl,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
)

func init() {
	// Let cobra ignore if we are running as "GUI" or not
	cobra.MousetrapHelpText = ""

	databaseRootDir = rootCmd.PersistentFlags().String("db", "", "set database directory")
	err := rootCmd.MarkPersistentFlagRequired("db")
	if err != nil {
		panic(err)
	}
}

func main() {
	var err error

	// check if we are running in a console (try to attach to parent console if available)
	runningInConsole, err = attachToParentConsole()
	if err != nil {
		fmt.Printf("failed to attach to parent console: %s\n", err)
		os.Exit(1)
	}

	// not using portbase logger
	log.SetLogLevel(log.CriticalLevel)

	// for debugging
	// log.Start()
	// log.SetLogLevel(log.TraceLevel)
	// go func() {
	// 	time.Sleep(3 * time.Second)
	// 	pprof.Lookup("goroutine").WriteTo(os.Stdout, 2)
	// 	os.Exit(1)
	// }()

	// set meta info
	info.Set("Portmaster Control", "0.2.5", "AGPLv3", true)

	// check if meta info is ok
	err = info.CheckVersion()
	if err != nil {
		fmt.Printf("%s compile error: please compile using the provided build script\n", logPrefix)
		os.Exit(1)
	}

	// react to version flag
	if info.PrintVersion() {
		os.Exit(0)
	}

	// start root command
	if err = rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func initPmCtl(cmd *cobra.Command, args []string) (err error) {
	// transform from db base path to updates path
	if *databaseRootDir != "" {
		updates.SetDatabaseRoot(*databaseRootDir)
		updateStoragePath = filepath.Join(*databaseRootDir, "updates")
	} else {
		return errors.New("please supply the database directory using the --db flag")
	}

	return nil
}
