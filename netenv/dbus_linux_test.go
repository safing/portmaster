package netenv

import (
	"errors"
	"io/fs"
	"os"
	"testing"
)

func TestDbus(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("skipping test in short mode because it fails in the CI")
	}

	if _, err := os.Stat("/var/run/dbus/system_bus_socket"); errors.Is(err, fs.ErrNotExist) {
		t.Logf("skipping dbus tests, as dbus does not seem to be installed: %s", err)
		return
	}

	nameservers, err := getNameserversFromDbus()
	if err != nil {
		t.Errorf("getNameserversFromDbus failed: %s", err)
	}
	t.Logf("getNameserversFromDbus: %v", nameservers)

	connectivityState, err := getConnectivityStateFromDbus()
	if err != nil {
		t.Errorf("getConnectivityStateFromDbus failed: %s", err)
	}
	t.Logf("getConnectivityStateFromDbus: %v", connectivityState)
}
