package main

import (
	"context"
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
	dataDir    string
	maxRetries int
	dataRoot   *utils.DirStructure
	logsRoot   *utils.DirStructure

	// create registry
	registry = &updater.ResourceRegistry{
		Name: "updates",
		UpdateURLs: []string{
			"https://updates.safing.io",
		},
		Beta:    false,
		DevMode: false,
		Online:  true, // is disabled later based on command
	}

	rootCmd = &cobra.Command{
		Use:   "portmaster-start",
		Short: "Start Portmaster components",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) (err error) {

			if err := configureDataRoot(); err != nil {
				return err
			}

			if err := configureLogging(); err != nil {
				return err
			}

			return nil
		},
		SilenceUsage: true,
	}
)

func init() {
	// Let cobra ignore if we are running as "GUI" or not
	cobra.MousetrapHelpText = ""

	flags := rootCmd.PersistentFlags()
	{
		flags.StringVar(&dataDir, "data", "", "Configures the data directory. Alternatively, this can also be set via the environment variable PORTMASTER_DATA.")
		flags.IntVar(&maxRetries, "max-retries", 5, "Maximum number of retries when starting a Portmaster component")
		flags.BoolVar(&stdinSignals, "input-signals", false, "Emulate signals using stdid.")
		_ = rootCmd.MarkPersistentFlagDirname("data")
		_ = flags.MarkHidden("input-signals")
	}
}

func main() {
	cobra.OnInitialize(initCobra)

	// set meta info
	info.Set("Portmaster Start", "0.4.0", "AGPLv3", false)

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

	// wait for signals
	for sig := range signalCh {
		if childIsRunning.IsSet() {
			log.Printf("got %s signal (ignoring), waiting for child to exit...\n", sig)
			continue
		}

		log.Printf("got %s signal, exiting... (not executing anything)\n", sig)
		os.Exit(0)
	}
}

func initCobra() {
	// check if we are running in a console (try to attach to parent console if available)
	var err error
	runningInConsole, err = attachToParentConsole()
	if err != nil {
		log.Fatalf("failed to attach to parent console: %s\n", err)
	}

	// check if meta info is ok
	err = info.CheckVersion()
	if err != nil {
		log.Fatalf("compile error: please compile using the provided build script")
	}

	// set up logging
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
	log.SetPrefix("[control] ")
	log.SetOutput(os.Stdout)

	// not using portbase logger
	portlog.SetLogLevel(portlog.CriticalLevel)
}

func configureDataRoot() error {
	// The data directory is not
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
	err := dataroot.Initialize(dataDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to initialize data root: %s", err)
	}
	dataRoot = dataroot.Root()

	// initialize registry
	err = registry.Initialize(dataRoot.ChildDir("updates", 0755))
	if err != nil {
		return err
	}

	registry.AddIndex(updater.Index{
		Path:   "stable.json",
		Stable: true,
		Beta:   false,
	})

	// TODO: enable loading beta versions
	// registry.AddIndex(updater.Index{
	// Path:   "beta.json",
	// Stable: false,
	// Beta:   true,
	// })

	updateRegistryIndex()
	return nil
}

func configureLogging() error {
	// set up logs root
	logsRoot = dataRoot.ChildDir("logs", 0777)
	err := logsRoot.Ensure()
	if err != nil {
		return fmt.Errorf("failed to initialize logs root: %s", err)
	}

	// warn about CTRL-C on windows
	if runningInConsole && onWindows {
		log.Println("WARNING: portmaster-start is marked as a GUI application in order to get rid of the console window.")
		log.Println("WARNING: CTRL-C will immediately kill without clean shutdown.")
	}
	return nil
}

func updateRegistryIndex() {
	err := registry.LoadIndexes(context.Background())
	if err != nil {
		log.Printf("WARNING: error loading indexes: %s\n", err)
	}

	err = registry.ScanStorage("")
	if err != nil {
		log.Printf("WARNING: error during storage scan: %s\n", err)
	}

	registry.SelectVersions()
}
