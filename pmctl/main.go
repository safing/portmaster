package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/safing/portbase/dataroot"
	"github.com/safing/portbase/info"
	portlog "github.com/safing/portbase/log"
	"github.com/safing/portbase/updater"
	"github.com/safing/portbase/utils"

	"github.com/spf13/cobra"
)

var (
	dataDir     string
	databaseDir string
	dataRoot    *utils.DirStructure
	logsRoot    *utils.DirStructure

	showShortVersion bool
	showFullVersion  bool

	// create registry
	registry = &updater.ResourceRegistry{
		Name: "updates",
		UpdateURLs: []string{
			"https://updates.safing.io",
		},
		Beta:    false,
		DevMode: false,
		Online:  false,
	}

	rootCmd = &cobra.Command{
		Use:               "portmaster-control",
		Short:             "Controller for all portmaster components",
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
		SilenceUsage: true,
	}
)

func init() {
	// Let cobra ignore if we are running as "GUI" or not
	cobra.MousetrapHelpText = ""

	rootCmd.PersistentFlags().StringVar(&dataDir, "data", "", "Configures the data directory. Alternatively, this can also be set via the environment variable PORTMASTER_DATA.")
	rootCmd.PersistentFlags().StringVar(&databaseDir, "db", "", "Alias to --data (deprecated)")
	_ = rootCmd.MarkPersistentFlagDirname("data")
	_ = rootCmd.MarkPersistentFlagDirname("db")
	rootCmd.Flags().BoolVar(&showFullVersion, "version", false, "Print version of portmaster-control.")
	rootCmd.Flags().BoolVar(&showShortVersion, "ver", false, "Print version number only")
}

func main() {
	// set meta info
	info.Set("Portmaster Control", "0.3.2", "AGPLv3", true)

	// for debugging
	// log.Start()
	// log.SetLogLevel(log.TraceLevel)
	// go func() {
	// 	time.Sleep(3 * time.Second)
	// 	pprof.Lookup("goroutine").WriteTo(os.Stdout, 2)
	// 	os.Exit(1)
	// }()

	// catch interrupt for clean shutdown
	signalCh := make(chan os.Signal, 2)
	signal.Notify(
		signalCh,
		os.Interrupt,
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

	// data directory
	if !showShortVersion && !showFullVersion {
		// set data root
		// backwards compatibility
		if dataDir == "" {
			dataDir = databaseDir
		}

		// check for environment variable
		// PORTMASTER_DATA
		if dataDir == "" {
			dataDir = os.Getenv("PORTMASTER_DATA")
		}

		// check data dir
		if dataDir == "" {
			return errors.New("please set the data directory using --data=/path/to/data/dir")
		}

		// remove redundant escape characters and quotes
		dataDir = strings.Trim(dataDir, `\"`)
		// initialize dataroot
		err = dataroot.Initialize(dataDir, 0755)
		if err != nil {
			return fmt.Errorf("failed to initialize data root: %s", err)
		}
		dataRoot = dataroot.Root()

		// initialize registry
		err := registry.Initialize(dataRoot.ChildDir("updates", 0755))
		if err != nil {
			return err
		}

		registry.AddIndex(updater.Index{
			Path:   "stable.json",
			Stable: true,
			Beta:   false,
		})

		registry.AddIndex(updater.Index{
			Path:   "beta.json",
			Stable: false,
			Beta:   true,
		})

		registry.AddIndex(updater.Index{
			Path:   "all/intel/intel.json",
			Stable: true,
			Beta:   false,
		})

		updateRegistryIndex()
	}

	// logs and warning
	if !showShortVersion && !showFullVersion && !strings.Contains(cmd.CommandPath(), " show ") {
		// set up logs root
		logsRoot = dataRoot.ChildDir("logs", 0777)
		err = logsRoot.Ensure()
		if err != nil {
			return fmt.Errorf("failed to initialize logs root: %s", err)
		}

		// warn about CTRL-C on windows
		if runningInConsole && onWindows {
			log.Println("WARNING: portmaster-control is marked as a GUI application in order to get rid of the console window.")
			log.Println("WARNING: CTRL-C will immediately kill without clean shutdown.")
		}
	}

	return nil
}

func updateRegistryIndex() {
	err := registry.LoadIndexes()
	if err != nil {
		log.Printf("WARNING: error loading indexes: %s\n", err)
	}

	err = registry.ScanStorage("")
	if err != nil {
		log.Printf("WARNING: error during storage scan: %s\n", err)
	}

	registry.SelectVersions()
}
