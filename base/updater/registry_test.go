package updater

import (
	"os"
	"testing"

	"github.com/safing/portmaster/base/utils"
)

var registry *ResourceRegistry

func TestMain(m *testing.M) {
	// setup
	tmpDir, err := os.MkdirTemp("", "ci-portmaster-")
	if err != nil {
		panic(err)
	}
	registry = &ResourceRegistry{
		UsePreReleases: true,
		DevMode:        true,
		Online:         true,
	}
	err = registry.Initialize(utils.NewDirStructure(tmpDir, utils.PublicWritePermission))
	if err != nil {
		panic(err)
	}

	// run
	// call flag.Parse() here if TestMain uses flags
	ret := m.Run()

	// teardown
	_ = os.RemoveAll(tmpDir)
	os.Exit(ret)
}
