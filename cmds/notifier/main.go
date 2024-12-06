package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/api/client"
	"github.com/safing/portmaster/base/dataroot"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/updater"
	"github.com/safing/portmaster/base/utils"
	"github.com/safing/portmaster/service/updates/helper"
)

var (
	dataDir          string
	printStackOnExit bool
	showVersion      bool

	apiClient    = client.NewClient("127.0.0.1:817")
	connected    = abool.New()
	shuttingDown = abool.New()
	restarting   = abool.New()

	mainCtx, cancelMainCtx = context.WithCancel(context.Background())
	mainWg                 = &sync.WaitGroup{}

	dataRoot *utils.DirStructure
	// Create registry.
	registry = &updater.ResourceRegistry{
		Name: "updates",
		UpdateURLs: []string{
			"https://updates.safing.io",
		},
		DevMode: false,
		Online:  false, // disable download of resources (this is job for the core).
	}
)

const query = "query "

func init() {
	flag.StringVar(&dataDir, "data", "", "set data directory")
	flag.BoolVar(&printStackOnExit, "print-stack-on-exit", false, "prints the stack before of shutting down")
	flag.BoolVar(&showVersion, "version", false, "show version and exit")

	runtime.GOMAXPROCS(2)
}

func main() {
	// parse flags
	flag.Parse()

	// set meta info
	info.Set("Portmaster Notifier", "0.3.6", "GPLv3")

	// check if meta info is ok
	err := info.CheckVersion()
	if err != nil {
		fmt.Println("compile error: please compile using the provided build script")
		os.Exit(1)
	}

	// print help
	// if modules.HelpFlag {
	// 	flag.Usage()
	// 	os.Exit(0)
	// }

	if showVersion {
		fmt.Println(info.FullVersion())
		os.Exit(0)
	}

	// auto detect
	if dataDir == "" {
		dataDir = detectDataDir()
	}

	// check data dir
	if dataDir == "" {
		fmt.Fprintln(os.Stderr, "please set the data directory using --data=/path/to/data/dir")
		os.Exit(1)
	}

	// switch to safe exec dir
	err = os.Chdir(filepath.Join(dataDir, "exec"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to switch to safe exec dir: %s\n", err)
	}

	// start log writer
	err = log.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start logging: %s\n", err)
		os.Exit(1)
	}

	// load registry
	err = configureRegistry(true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load registry: %s\n", err)
		os.Exit(1)
	}

	// connect to API
	go apiClient.StayConnected()
	go apiStatusMonitor()

	// start subsystems
	go tray()
	go subsystemsClient()
	go spnStatusClient()
	go notifClient()
	go startShutdownEventListener()

	// Shutdown
	// catch interrupt for clean shutdown
	signalCh := make(chan os.Signal, 1)
	signal.Notify(
		signalCh,
		os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)

	// wait for shutdown
	select {
	case <-signalCh:
		fmt.Println(" <INTERRUPT>")
		log.Warning("program was interrupted, shutting down")
	case <-mainCtx.Done():
		log.Warning("program is shutting down")
	}

	if printStackOnExit {
		fmt.Println("=== PRINTING STACK ===")
		_ = pprof.Lookup("goroutine").WriteTo(os.Stdout, 2)
		fmt.Println("=== END STACK ===")
	}
	go func() {
		time.Sleep(10 * time.Second)
		fmt.Println("===== TAKING TOO LONG FOR SHUTDOWN - PRINTING STACK TRACES =====")
		_ = pprof.Lookup("goroutine").WriteTo(os.Stdout, 2)
		os.Exit(1)
	}()

	// clear all notifications
	clearNotifications()

	// shutdown
	cancelMainCtx()
	mainWg.Wait()

	apiClient.Shutdown()
	exitTray()
	log.Shutdown()

	os.Exit(0)
}

func apiStatusMonitor() {
	for {
		// Wait for connection.
		<-apiClient.Online()
		connected.Set()
		triggerTrayUpdate()

		// Wait for lost connection.
		<-apiClient.Offline()
		connected.UnSet()
		triggerTrayUpdate()
	}
}

func detectDataDir() string {
	// get path of executable
	binPath, err := os.Executable()
	if err != nil {
		return ""
	}
	// get directory
	binDir := filepath.Dir(binPath)
	// check if we in the updates directory
	identifierDir := filepath.Join("updates", runtime.GOOS+"_"+runtime.GOARCH, "notifier")
	// check if there is a match and return data dir
	if strings.HasSuffix(binDir, identifierDir) {
		return filepath.Clean(strings.TrimSuffix(binDir, identifierDir))
	}
	return ""
}

func configureRegistry(mustLoadIndex bool) error {
	// If dataDir is not set, check the environment variable.
	if dataDir == "" {
		dataDir = os.Getenv("PORTMASTER_DATA")
	}

	// If it's still empty, try to auto-detect it.
	if dataDir == "" {
		dataDir = detectInstallationDir()
	}

	// Finally, if it's still empty, the user must provide it.
	if dataDir == "" {
		return errors.New("please set the data directory using --data=/path/to/data/dir")
	}

	// Remove left over quotes.
	dataDir = strings.Trim(dataDir, `\"`)
	// Initialize data root.
	err := dataroot.Initialize(dataDir, utils.PublicReadPermission)
	if err != nil {
		return fmt.Errorf("failed to initialize data root: %w", err)
	}
	dataRoot = dataroot.Root()

	// Initialize registry.
	err = registry.Initialize(dataRoot.ChildDir("updates", utils.PublicReadPermission))
	if err != nil {
		return err
	}

	return updateRegistryIndex(mustLoadIndex)
}

func detectInstallationDir() string {
	exePath, err := filepath.Abs(os.Args[0])
	if err != nil {
		return ""
	}

	parent := filepath.Dir(exePath)                                    // parent should be "...\updates\windows_amd64\notifier"
	stableJSONFile := filepath.Join(parent, "..", "..", "stable.json") // "...\updates\stable.json"
	stat, err := os.Stat(stableJSONFile)
	if err != nil {
		return ""
	}

	if stat.IsDir() {
		return ""
	}

	return parent
}

func updateRegistryIndex(mustLoadIndex bool) error {
	// Set indexes based on the release channel.
	warning := helper.SetIndexes(registry, "", false, false, false)
	if warning != nil {
		log.Warningf("%q", warning)
	}

	// Load indexes from disk or network, if needed and desired.
	err := registry.LoadIndexes(context.Background())
	if err != nil {
		log.Warningf("error loading indexes %q", warning)
		if mustLoadIndex {
			return err
		}
	}

	// Load versions from disk to know which others we have and which are available.
	err = registry.ScanStorage("")
	if err != nil {
		log.Warningf("error during storage scan: %q\n", err)
	}

	registry.SelectVersions()
	return nil
}
