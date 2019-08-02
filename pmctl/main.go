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

	"github.com/safing/portmaster/core/structure"
	"github.com/safing/portmaster/updates"

	"github.com/safing/portbase/utils"

	"github.com/safing/portbase/info"
	portlog "github.com/safing/portbase/log"
	"github.com/spf13/cobra"
)

var (
	dataDir     string
	databaseDir string
	dataRoot    *utils.DirStructure
	logsRoot    *utils.DirStructure

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

	rootCmd.PersistentFlags().StringVar(&dataDir, "data", "", "set data directory")
	rootCmd.PersistentFlags().StringVar(&databaseDir, "db", "", "alias to --data (deprecated)")
	rootCmd.Flags().BoolVar(&showFullVersion, "version", false, "print version")
	rootCmd.Flags().BoolVar(&showShortVersion, "ver", false, "print version number only")
}

func main() {
	// set meta info
	info.Set("Portmaster Control", "0.2.9", "AGPLv3", true)

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
		// set data root
		// backwards compatibility
		if dataDir == "" {
			dataDir = databaseDir
		}
		// check data dir
		if dataDir == "" {
			return errors.New("please set the data directory using --data=/path/to/data/dir")
		}
		// remove redundant escape characters and quotes
		dataDir = strings.Trim(dataDir, `\"`)
		// initialize structure
		err = structure.Initialize(dataDir, 0755)
		if err != nil {
			return fmt.Errorf("failed to initialize data root: %s", err)
		}
		dataRoot = structure.Root()
		// manually set updates root (no modules)
		updates.SetDataRoot(structure.Root())
		// set up logs root
		logsRoot = structure.NewRootDir("logs", 0777)
		err = logsRoot.Ensure()
		if err != nil {
			return fmt.Errorf("failed to initialize logs root: %s", err)
		}

		// warn about CTRL-C on windows
		if runningInConsole && runtime.GOOS == "windows" {
			log.Println("WARNING: portmaster-control is marked as a GUI application in order to get rid of the console window.")
			log.Println("WARNING: CTRL-C will immediately kill without clean shutdown.")
		}
	}

	return nil
}
