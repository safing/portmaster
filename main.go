package main

import (
	"os"

	"github.com/safing/portbase/info"
	"github.com/safing/portbase/run"

	// include packages here
	_ "github.com/safing/portmaster/core"
	_ "github.com/safing/portmaster/firewall"
	_ "github.com/safing/portmaster/nameserver"
	_ "github.com/safing/portmaster/ui"
)

func main() {
	/*go func() {
		time.Sleep(10 * time.Second)
		fmt.Fprintln(os.Stderr, "===== TAKING TOO LONG FOR SHUTDOWN - PRINTING STACK TRACES =====")
		_ = pprof.Lookup("goroutine").WriteTo(os.Stderr, 2)
		os.Exit(1)
	}()*/

	info.Set("Portmaster", "0.3.9", "AGPLv3", true)
	os.Exit(run.Run())
}
