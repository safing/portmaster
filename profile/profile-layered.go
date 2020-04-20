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

	localProfile    *Profile
	layers          []*Profile
	revisionCounter uint64

	validityFlag       *abool.AtomicBool
	validityFlagLock   sync.Mutex
	globalValidityFlag *config.ValidityFlag

	securityLevel *uint32

	DisableAutoPermit   config.BoolOption
	BlockScopeLocal     config.BoolOption
	BlockScopeLAN       config.BoolOption
	BlockScopeInternet  config.BoolOption
	BlockP2P            config.BoolOption
	BlockInbound        config.BoolOption
	EnforceSPN          config.BoolOption
	RemoveOutOfScopeDNS config.BoolOption
	RemoveBlockedDNS    config.BoolOption
	FilterSubDomains    config.BoolOption
	FilterCNAMEs        config.BoolOption
	PreventBypassing    config.BoolOption
}

// NewLayeredProfile returns a new layered profile based on the given local profile.
func NewLayeredProfile(localProfile *Profile) *LayeredProfile {
	var securityLevelVal uint32

	new := &LayeredProfile{
		localProfile:       localProfile,
		layers:             make([]*Profile, 0, len(localProfile.LinkedProfiles)+1),
		revisionCounter:    0,
		validityFlag:       abool.NewBool(true),
		globalValidityFlag: config.NewValidityFlag(),
		securityLevel:      &securityLevelVal,
	}

	new.DisableAutoPermit = new.wrapSecurityLevelOption(
		CfgOptionDisableAutoPermitKey,
		cfgOptionDisableAutoPermit,
	)
	new.BlockScopeLocal = new.wrapSecurityLevelOption(
		CfgOptionBlockScopeLocalKey,
		cfgOptionBlockScopeLocal,
	)
	new.BlockScopeLAN = new.wrapSecurityLevelOption(
		CfgOptionBlockScopeLANKey,
		cfgOptionBlockScopeLAN,
	)
	new.BlockScopeInternet = new.wrapSecurityLevelOption(
		CfgOptionBlockScopeInternetKey,
		cfgOptionBlockScopeInternet,
	)
	new.BlockP2P = new.wrapSecurityLevelOption(
		CfgOptionBlockP2PKey,
		cfgOptionBlockP2P,
	)
	new.BlockInbound = new.wrapSecurityLevelOption(
		CfgOptionBlockInboundKey,
		cfgOptionBlockInbound,
	)
	new.EnforceSPN = new.wrapSecurityLevelOption(
		CfgOptionEnforceSPNKey,
		cfgOptionEnforceSPN,
	)
	new.RemoveOutOfScopeDNS = new.wrapSecurityLevelOption(
		CfgOptionRemoveOutOfScopeDNSKey,
		cfgOptionRemoveOutOfScopeDNS,
	)
	new.RemoveBlockedDNS = new.wrapSecurityLevelOption(
		CfgOptionRemoveBlockedDNSKey,
		cfgOptionRemoveBlockedDNS,
	)
	new.FilterSubDomains = new.wrapSecurityLevelOption(
		CfgOptionFilterSubDomainsKey,
		cfgOptionFilterSubDomains,
	)
	new.FilterCNAMEs = new.wrapSecurityLevelOption(
		CfgOptionFilterCNAMEKey,
		cfgOptionFilterCNAME,
	)
	new.PreventBypassing = new.wrapSecurityLevelOption(
		CfgOptionPreventBypassingKey,
		cfgOptionPreventBypassing,
	)

	// TODO: load linked profiles.

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
func (lp *LayeredProfile) Update() (revisionCounter uint64) {
	lp.lock.Lock()
	defer lp.lock.Unlock()

	var changed bool
	for i, layer := range lp.layers {
		if layer.outdated.IsSet() {
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

		// update cached data fields
		lp.updateCaches()

		// bump revision counter
		lp.revisionCounter++
	}

	return lp.revisionCounter
}

func (lp *LayeredProfile) updateCaches() {
	// update security level
	var newLevel uint8
	for _, layer := range lp.layers {
		if newLevel < layer.SecurityLevel {
			newLevel = layer.SecurityLevel
		}
	}
	atomic.StoreUint32(lp.securityLevel, uint32(newLevel))

	// TODO: ignore community profiles
}

// MarkUsed marks the localProfile as used.
func (lp *LayeredProfile) MarkUsed() {
	lp.localProfile.MarkUsed()
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
func (lp *LayeredProfile) MatchEndpoint(entity *intel.Entity) (endpoints.EPResult, endpoints.Reason) {
	for _, layer := range lp.layers {
		if layer.endpoints.IsSet() {
			result, reason := layer.endpoints.Match(entity)
			if endpoints.IsDecision(result) {
				return result, reason
			}
		}
	}

	cfgLock.RLock()
	defer cfgLock.RUnlock()
	return cfgEndpoints.Match(entity)
}

// MatchServiceEndpoint checks if the given endpoint of an inbound connection matches an entry in any of the profiles.
func (lp *LayeredProfile) MatchServiceEndpoint(entity *intel.Entity) (endpoints.EPResult, endpoints.Reason) {
	entity.EnableReverseResolving()

	for _, layer := range lp.layers {
		if layer.serviceEndpoints.IsSet() {
			result, reason := layer.serviceEndpoints.Match(entity)
			if endpoints.IsDecision(result) {
				return result, reason
			}
		}
	}

	cfgLock.RLock()
	defer cfgLock.RUnlock()
	return cfgServiceEndpoints.Match(entity)
}

// MatchFilterLists matches the entity against the set of filter
// lists.
func (lp *LayeredProfile) MatchFilterLists(entity *intel.Entity) (endpoints.EPResult, endpoints.Reason) {
	entity.ResolveSubDomainLists(lp.FilterSubDomains())
	entity.EnableCNAMECheck(lp.FilterCNAMEs())

	for _, layer := range lp.layers {
		// search for the first layer that has filterListIDs set
		if len(layer.filterListIDs) > 0 {
			entity.LoadLists()

			if entity.MatchLists(layer.filterListIDs) {
				return endpoints.Denied, entity.ListBlockReason()
			}

			return endpoints.NoMatch, nil
		}
	}

	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if len(cfgFilterLists) > 0 {
		entity.LoadLists()

		if entity.MatchLists(cfgFilterLists) {
			return endpoints.Denied, entity.ListBlockReason()
		}
	}

	return endpoints.NoMatch, nil
}

// AddEndpoint adds an endpoint to the local endpoint list, saves the local profile and reloads the configuration.
func (lp *LayeredProfile) AddEndpoint(newEntry string) {
	lp.localProfile.AddEndpoint(newEntry)
}

// AddServiceEndpoint adds a service endpoint to the local endpoint list, saves the local profile and reloads the configuration.
func (lp *LayeredProfile) AddServiceEndpoint(newEntry string) {
	lp.localProfile.AddServiceEndpoint(newEntry)
}

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
