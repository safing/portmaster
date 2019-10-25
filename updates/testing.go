package updates

import (
	"os"
	"path/filepath"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/updater"
	"github.com/safing/portbase/utils"
)

// InitForTesting initializes the update module directly. This is intended to be only used by unit tests that require the updates module.
func InitForTesting() error {
	// create registry
	registry = &updater.ResourceRegistry{
		Name: "testing-updates",
		UpdateURLs: []string{
			"https://updates.safing.io",
		},
		Beta:    false,
		DevMode: false,
		Online:  true,
	}

	// set data dir
	root := utils.NewDirStructure(
		filepath.Join(os.TempDir(), "pm-testing"),
		0755,
	)
	err := root.Ensure()
	if err != nil {
		return err
	}

	// initialize
	err = registry.Initialize(root.ChildDir("updates", 0755))
	if err != nil {
		return err
	}

	err = registry.LoadIndexes()
	if err != nil {
		return err
	}

	err = registry.ScanStorage("")
	if err != nil {
		log.Warningf("updates: error during storage scan: %s", err)
	}

	registry.SelectVersions()
	return nil
}
