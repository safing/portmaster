package profile

import (
	"sync"
	"sync/atomic"

	"github.com/safing/portbase/log"

	"github.com/safing/portmaster/status"

	"github.com/tevino/abool"

	"github.com/safing/portbase/config"
	"github.com/safing/portmaster/intel"
	"github.com/safing/portmaster/profile/endpoints"
)

var (
	no = abool.NewBool(false)
)

// LayeredProfile combines multiple Profiles.
type LayeredProfile struct {
	lock sync.Mutex

	localProfile *Profile
	layers       []*Profile

	validityFlag       *abool.AtomicBool
	validityFlagLock   sync.Mutex
	globalValidityFlag *config.ValidityFlag

	securityLevel *uint32

	DisableAutoPermit  config.BoolOption
	BlockScopeLocal    config.BoolOption
	BlockScopeLAN      config.BoolOption
	BlockScopeInternet config.BoolOption
	BlockP2P           config.BoolOption
	BlockInbound       config.BoolOption
	EnforceSPN         config.BoolOption
}

// NewLayeredProfile returns a new layered profile based on the given local profile.
func NewLayeredProfile(localProfile *Profile) *LayeredProfile {
	var securityLevelVal uint32

	new := &LayeredProfile{
		localProfile:       localProfile,
		layers:             make([]*Profile, 0, len(localProfile.LinkedProfiles)+1),
		validityFlag:       abool.NewBool(true),
		globalValidityFlag: config.NewValidityFlag(),
		securityLevel:      &securityLevelVal,
	}

	new.DisableAutoPermit = new.wrapSecurityLevelOption(
		cfgOptionDisableAutoPermitKey,
		cfgOptionDisableAutoPermit,
	)
	new.BlockScopeLocal = new.wrapSecurityLevelOption(
		cfgOptionBlockScopeLocalKey,
		cfgOptionBlockScopeLocal,
	)
	new.BlockScopeLAN = new.wrapSecurityLevelOption(
		cfgOptionBlockScopeLANKey,
		cfgOptionBlockScopeLAN,
	)
	new.BlockScopeInternet = new.wrapSecurityLevelOption(
		cfgOptionBlockScopeInternetKey,
		cfgOptionBlockScopeInternet,
	)
	new.BlockP2P = new.wrapSecurityLevelOption(
		cfgOptionBlockP2PKey,
		cfgOptionBlockP2P,
	)
	new.BlockInbound = new.wrapSecurityLevelOption(
		cfgOptionBlockInboundKey,
		cfgOptionBlockInbound,
	)
	new.EnforceSPN = new.wrapSecurityLevelOption(
		cfgOptionEnforceSPNKey,
		cfgOptionEnforceSPN,
	)

	// TODO: load referenced profiles

	// FUTURE: load forced company profile
	new.layers = append(new.layers, localProfile)
	// FUTURE: load company profile
	// FUTURE: load community profile

	new.updateCaches()
	return new
}

func (lp *LayeredProfile) getValidityFlag() *abool.AtomicBool {
	lp.validityFlagLock.Lock()
	defer lp.validityFlagLock.Unlock()
	return lp.validityFlag
}

// Update checks for updated profiles and replaces any outdated profiles.
func (lp *LayeredProfile) Update() {
	lp.lock.Lock()
	defer lp.lock.Lock()

	var changed bool
	for i, layer := range lp.layers {
		if layer.oudated.IsSet() {
			changed = true
			// update layer
			newLayer, err := GetProfile(layer.Source, layer.ID)
			if err != nil {
				log.Errorf("profiles: failed to update profile %s", layer.ScopedID())
			} else {
				lp.layers[i] = newLayer
			}
		}
	}
	if !lp.globalValidityFlag.IsValid() {
		changed = true
	}

	if changed {
		// reset validity flag
		lp.validityFlagLock.Lock()
		lp.validityFlag.SetTo(false)
		lp.validityFlag = abool.NewBool(true)
		lp.validityFlagLock.Unlock()
		// get global config validity flag
		lp.globalValidityFlag.Refresh()

		lp.updateCaches()
	}
}

func (lp *LayeredProfile) updateCaches() {
	// update security level
	var newLevel uint8 = 0
	for _, layer := range lp.layers {
		if newLevel < layer.SecurityLevel {
			newLevel = layer.SecurityLevel
		}
	}
	atomic.StoreUint32(lp.securityLevel, uint32(newLevel))

	// TODO: ignore community profiles
}

// SecurityLevel returns the highest security level of all layered profiles.
func (lp *LayeredProfile) SecurityLevel() uint8 {
	return uint8(atomic.LoadUint32(lp.securityLevel))
}

// DefaultAction returns the active default action ID.
func (lp *LayeredProfile) DefaultAction() uint8 {
	for _, layer := range lp.layers {
		if layer.defaultAction > 0 {
			return layer.defaultAction
		}
	}

	cfgLock.RLock()
	defer cfgLock.RUnlock()
	return cfgDefaultAction
}

// MatchEndpoint checks if the given endpoint matches an entry in any of the profiles.
func (lp *LayeredProfile) MatchEndpoint(entity *intel.Entity) (result endpoints.EPResult, reason string) {
	for _, layer := range lp.layers {
		if layer.endpoints.IsSet() {
			result, reason = layer.endpoints.Match(entity)
			if result != endpoints.NoMatch {
				return
			}
		}
	}

	cfgLock.RLock()
	defer cfgLock.RUnlock()
	return cfgEndpoints.Match(entity)
}

// MatchServiceEndpoint checks if the given endpoint of an inbound connection matches an entry in any of the profiles.
func (lp *LayeredProfile) MatchServiceEndpoint(entity *intel.Entity) (result endpoints.EPResult, reason string) {
	entity.EnableReverseResolving()

	for _, layer := range lp.layers {
		if layer.serviceEndpoints.IsSet() {
			result, reason = layer.serviceEndpoints.Match(entity)
			if result != endpoints.NoMatch {
				return
			}
		}
	}

	cfgLock.RLock()
	defer cfgLock.RUnlock()
	return cfgServiceEndpoints.Match(entity)
}

/*
func (lp *LayeredProfile) wrapSecurityLevelOption(configKey string, globalConfig config.IntOption) config.BoolOption {
	valid := no
	var activeAtLevels uint8

	return func() bool {
		if !valid.IsSet() {
			valid = lp.getValidityFlag()

			found := false
		layerLoop:
			for _, layer := range lp.layers {
				layerLevel, ok := layer.configPerspective.GetAsInt(configKey)
				if ok {
					found = true
					// TODO: add instead?
					activeAtLevels = uint8(layerLevel)
					break layerLoop
				}
			}
			if !found {
				activeAtLevels = uint8(globalConfig())
			}
		}

		return activeAtLevels&max(
			lp.SecurityLevel(),           // layered profile security level
			status.ActiveSecurityLevel(), // global security level
		) > 0
	}
}
*/

func (lp *LayeredProfile) wrapSecurityLevelOption(configKey string, globalConfig config.IntOption) config.BoolOption {
	activeAtLevels := lp.wrapIntOption(configKey, globalConfig)

	return func() bool {
		return uint8(activeAtLevels())&max(
			lp.SecurityLevel(),           // layered profile security level
			status.ActiveSecurityLevel(), // global security level
		) > 0
	}
}

func (lp *LayeredProfile) wrapIntOption(configKey string, globalConfig config.IntOption) config.IntOption {
	valid := no
	var value int64

	return func() int64 {
		if !valid.IsSet() {
			valid = lp.getValidityFlag()

			found := false
		layerLoop:
			for _, layer := range lp.layers {
				layerValue, ok := layer.configPerspective.GetAsInt(configKey)
				if ok {
					found = true
					value = layerValue
					break layerLoop
				}
			}
			if !found {
				value = globalConfig()
			}
		}

		return value
	}
}

/*
For later:

func (lp *LayeredProfile) wrapStringOption(configKey string, globalConfig config.StringOption) config.StringOption {
	valid := no
	var value string

	return func() string {
		if !valid.IsSet() {
			valid = lp.getValidityFlag()

			found := false
		layerLoop:
			for _, layer := range lp.layers {
				layerValue, ok := layer.configPerspective.GetAsString(configKey)
				if ok {
					found = true
					value = layerValue
					break layerLoop
				}
			}
			if !found {
				value = globalConfig()
			}
		}

		return value
	}
}
*/

func max(a, b uint8) uint8 {
	if a > b {
		return a
	}
	return b
}
