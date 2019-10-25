package intel

import (
	"os"
	"testing"

	"github.com/safing/portmaster/core"
)

func TestMain(m *testing.M) {
	// setup
	tmpDir, err := core.InitForTesting()
	if err != nil {
		panic(err)
	}

	// setup package
	err = prep()
	if err != nil {
		panic(err)
	}
	loadResolvers()

	// run tests
	rv := m.Run()

	// teardown
	core.StopTesting()
	_ = os.RemoveAll(tmpDir)

	// exit with test run return value
	os.Exit(rv)
}
