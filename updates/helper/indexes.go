package helper

import (
	"github.com/safing/portbase/updater"
)

const (
	ReleaseChannelKey     = "core/releaseChannel"
	ReleaseChannelJSONKey = "core.releaseChannel"
	ReleaseChannelStable  = "stable"
	ReleaseChannelBeta    = "beta"
	ReleaseChannelStaging = "staging"
	ReleaseChannelSpecial = "special"
)

func SetIndexes(registry *updater.ResourceRegistry, releaseChannel string) {
	// Be reminded that the order is important, as indexes added later will
	// override the current release from earlier indexes.

	// Reset indexes before adding them (again).
	registry.ResetIndexes()

	// Always add the stable index as a base.
	registry.AddIndex(updater.Index{
		Path: ReleaseChannelStable + ".json",
	})

	// Add beta index if in beta or staging channel.
	if releaseChannel == ReleaseChannelBeta ||
		releaseChannel == ReleaseChannelStaging {
		registry.AddIndex(updater.Index{
			Path:       ReleaseChannelBeta + ".json",
			PreRelease: true,
		})
	}

	// Add staging index if in staging channel.
	if releaseChannel == ReleaseChannelStaging {
		registry.AddIndex(updater.Index{
			Path:       ReleaseChannelStaging + ".json",
			PreRelease: true,
		})
	}

	// Add special index if in special channel.
	if releaseChannel == ReleaseChannelSpecial {
		registry.AddIndex(updater.Index{
			Path: ReleaseChannelSpecial + ".json",
		})
	}

	// Add the intel index last, as it updates the fastest and should not be
	// crippled by other faulty indexes. It can only specify versions for its
	// scope anyway.
	registry.AddIndex(updater.Index{
		Path: "all/intel/intel.json",
	})
}
