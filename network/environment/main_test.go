package environment

import (
	"os"
	"testing"

	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/core"
)

func TestMain(m *testing.M) {
	// setup
	tmpDir, err := core.InitForTesting()
	if err != nil {
		panic(err)
	}

	// setup package
	netModule := modules.Register("network", nil, nil, nil, "core")
	InitSubModule(netModule)
	err = StartSubModule()
	if err != nil {
		panic(err)
	}

	// run tests
	rv := m.Run()

	// teardown
	core.StopTesting()
	_ = os.RemoveAll(tmpDir)

	// exit with test run return value
	os.Exit(rv)
}
