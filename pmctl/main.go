package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Safing/portbase/info"
	"github.com/Safing/portbase/log"
	"github.com/Safing/portmaster/updates"
	"github.com/spf13/cobra"
)

const (
	logPrefix = "[pmctl]"
)

var (
	updateStoragePath string
	databaseRootDir   *string

	rootCmd = &cobra.Command{
		Use:               "pmctl",
		Short:             "contoller for all portmaster components",
		PersistentPreRunE: initPmCtl,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
)

func init() {
	databaseRootDir = rootCmd.PersistentFlags().String("db", "", "set database directory")
	err := rootCmd.MarkPersistentFlagRequired("db")
	if err != nil {
		panic(err)
	}
}

func main() {
	flag.Parse()

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
	info.Set("Portmaster Control", "0.1.3", "AGPLv3", true)

	// check if meta info is ok
	err := info.CheckVersion()
	if err != nil {
		fmt.Printf("%s compile error: please compile using the provided build script\n", logPrefix)
		os.Exit(1)
	}

	// react to version flag
	if info.PrintVersion() {
		os.Exit(0)
	}

	// start root command
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func initPmCtl(cmd *cobra.Command, args []string) error {

	// transform from db base path to updates path
	if *databaseRootDir != "" {
		updates.SetDatabaseRoot(*databaseRootDir)
		updateStoragePath = filepath.Join(*databaseRootDir, "updates")
	} else {
		return errors.New("please supply the database directory using the --db flag")
	}

	err := removeOldBin()
	if err != nil {
		fmt.Printf("%s warning: failed to remove old upgrade: %s\n", logPrefix, err)
	}

	update := checkForUpgrade()
	if update != nil {
		err = doSelfUpgrade(update)
		if err != nil {
			return fmt.Errorf("%s failed to upgrade self: %s", logPrefix, err)
		}
		fmt.Println("upgraded pmctl")
	}

	return nil
}
