package captain

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/updater"
	"github.com/safing/portmaster/service/updates"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/navigator"
	"github.com/safing/portmaster/spn/ships"
)

var (
	intelResource           *updater.File
	intelResourcePath       = "intel/spn/main-intel.yaml"
	intelResourceMapName    = "main"
	intelResourceUpdateLock sync.Mutex
)

func registerIntelUpdateHook() error {
	if err := module.RegisterEventHook(
		updates.ModuleName,
		updates.ResourceUpdateEvent,
		"update SPN intel",
		updateSPNIntel,
	); err != nil {
		return err
	}

	if err := module.RegisterEventHook(
		"config",
		config.ChangeEvent,
		"update SPN intel",
		updateSPNIntel,
	); err != nil {
		return err
	}

	return nil
}

func updateSPNIntel(ctx context.Context, _ interface{}) (err error) {
	intelResourceUpdateLock.Lock()
	defer intelResourceUpdateLock.Unlock()

	// Only update SPN intel when using the matching map.
	if conf.MainMapName != intelResourceMapName {
		return fmt.Errorf("intel resource not for map %q", conf.MainMapName)
	}

	// Check if there is something to do.
	if intelResource != nil && !intelResource.UpgradeAvailable() {
		return nil
	}

	// Get intel file and load it from disk.
	intelResource, err = updates.GetFile(intelResourcePath)
	if err != nil {
		return fmt.Errorf("failed to get SPN intel update: %w", err)
	}
	intelData, err := os.ReadFile(intelResource.Path())
	if err != nil {
		return fmt.Errorf("failed to load SPN intel update: %w", err)
	}

	// Parse and apply intel data.
	intel, err := hub.ParseIntel(intelData)
	if err != nil {
		return fmt.Errorf("failed to parse SPN intel update: %w", err)
	}

	setVirtualNetworkConfig(intel.VirtualNetworks)
	return navigator.Main.UpdateIntel(intel, cfgOptionTrustNodeNodes())
}

func resetSPNIntel() {
	intelResourceUpdateLock.Lock()
	defer intelResourceUpdateLock.Unlock()

	intelResource = nil
}

func setVirtualNetworkConfig(configs []*hub.VirtualNetworkConfig) {
	// Do nothing if not public Hub.
	if !conf.PublicHub() {
		return
	}
	// Reset if there are no virtual networks configured.
	if len(configs) == 0 {
		ships.SetVirtualNetworkConfig(nil)
	}

	// Check if we are in a virtual network.
	for _, config := range configs {
		if _, ok := config.Mapping[publicIdentity.Hub.ID]; ok {
			ships.SetVirtualNetworkConfig(config)
			return
		}
	}

	// If not, reset - we might have been in one before.
	ships.SetVirtualNetworkConfig(nil)
}
