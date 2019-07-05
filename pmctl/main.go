package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

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
	info.Set("Portmaster Control", "0.2.1", "AGPLv3", true)

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

	// check if we are root/admin for self upgrade
	userInfo, err := user.Current()
	if err != nil {
		return nil
	}
	switch runtime.GOOS {
	case "linux":
		if userInfo.Username != "root" {
			return nil
		}
	case "windows":
		if !strings.HasSuffix(userInfo.Username, "SYSTEM") { // is this correct?
			return nil
		}
	}

	err = removeOldBin()
	if err != nil {
		fmt.Printf("%s warning: failed to remove old upgrade: %s\n", logPrefix, err)
	}

	update := checkForUpgrade()
	if update != nil {
		err = doSelfUpgrade(update)
		if err != nil {
			return fmt.Errorf("%s failed to upgrade self: %s", logPrefix, err)
		}
		fmt.Println("upgraded portmaster-control")
	}

	return nil
}
