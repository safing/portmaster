package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/safing/portbase/info"
	portlog "github.com/safing/portbase/log"
	"github.com/safing/portmaster/updates"
	"github.com/spf13/cobra"
)

var (
	databaseRootDir  string
	showShortVersion bool
	showFullVersion  bool

	rootCmd = &cobra.Command{
		Use:               "portmaster-control",
		Short:             "contoller for all portmaster components",
		PersistentPreRunE: cmdSetup,
		RunE: func(cmd *cobra.Command, args []string) error {
			if showShortVersion {
				fmt.Println(info.Version())
				return nil
			}
			if showFullVersion {
				fmt.Println(info.FullVersion())
				return nil
			}
			return cmd.Help()
		},
	}
)

func init() {
	// Let cobra ignore if we are running as "GUI" or not
	cobra.MousetrapHelpText = ""

	rootCmd.PersistentFlags().StringVar(&databaseRootDir, "db", "", "set database directory")
	rootCmd.Flags().BoolVar(&showFullVersion, "version", false, "print version")
	rootCmd.Flags().BoolVar(&showShortVersion, "ver", false, "print version number only")
}

func main() {
	// set meta info
	info.Set("Portmaster Control", "0.2.5", "AGPLv3", true)

	// for debugging
	// log.Start()
	// log.SetLogLevel(log.TraceLevel)
	// go func() {
	// 	time.Sleep(3 * time.Second)
	// 	pprof.Lookup("goroutine").WriteTo(os.Stdout, 2)
	// 	os.Exit(1)
	// }()

	// catch interrupt for clean shutdown
	signalCh := make(chan os.Signal)
	signal.Notify(
		signalCh,
		os.Interrupt,
		os.Kill,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)

	// start root command
	go func() {
		if err := rootCmd.Execute(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}()

	// for debugging windows service (no stdout/err)
	// go func() {
	// 	time.Sleep(10 * time.Second)
	// 	// initiateShutdown(nil)
	// 	// logControlStack()
	// }()

	// wait for signals
	for sig := range signalCh {
		if childIsRunning.IsSet() {
			log.Printf("got %s signal (ignoring), waiting for child to exit...\n", sig)
		} else {
			log.Printf("got %s signal, exiting... (not executing anything)\n", sig)
			os.Exit(0)
		}
	}
}

func cmdSetup(cmd *cobra.Command, args []string) (err error) {
	// check if we are running in a console (try to attach to parent console if available)
	runningInConsole, err = attachToParentConsole()
	if err != nil {
		log.Printf("failed to attach to parent console: %s\n", err)
		os.Exit(1)
	}

	// check if meta info is ok
	err = info.CheckVersion()
	if err != nil {
		fmt.Println("compile error: please compile using the provided build script")
		os.Exit(1)
	}

	// set up logging
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
	log.SetPrefix("[control] ")
	log.SetOutput(os.Stdout)

	// not using portbase logger
	portlog.SetLogLevel(portlog.CriticalLevel)

	if !showShortVersion && !showFullVersion {
		// set database root
		if databaseRootDir != "" {
			// remove redundant escape characters and quotes
			databaseRootDir = strings.Trim(databaseRootDir, `\"`)
			// set updates path
			updates.SetDatabaseRoot(databaseRootDir)
		} else {
			return errors.New("please supply the database directory using the --db flag")
		}

		// warn about CTRL-C on windows
		if runningInConsole && runtime.GOOS == "windows" {
			log.Println("WARNING: portmaster-control is marked as a GUI application in order to get rid of the console window.")
			log.Println("WARNING: CTRL-C will immediately kill without clean shutdown.")
		}
	}

	return nil
}
