package environment

import (
	"os"
	"testing"
)

func TestDbus(t *testing.T) {
	if _, err := os.Stat("/var/run/dbus/system_bus_socket"); os.IsNotExist(err) {
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
