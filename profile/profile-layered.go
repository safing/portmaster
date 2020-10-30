package profile

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/runtime"

	"github.com/safing/portmaster/status"

	"github.com/safing/portbase/config"
	"github.com/safing/portmaster/intel"
	"github.com/safing/portmaster/profile/endpoints"
)

// LayeredProfile combines multiple Profiles.
type LayeredProfile struct {
	record.Base
	sync.RWMutex

	localProfile *Profile
	layers       []*Profile

	LayerIDs           []string
	RevisionCounter    uint64
	globalValidityFlag *config.ValidityFlag

	securityLevel *uint32

	// These functions give layered access to configuration options and require
	// the layered profile to be read locked.

	// TODO(ppacher): we need JSON tags here so the layeredProfile can be exposed
	// via the API. If we ever switch away from JSON to something else supported
	// by DSD this WILL BREAK!

	DisableAutoPermit   config.BoolOption `json:"-"`
	BlockScopeLocal     config.BoolOption `json:"-"`
	BlockScopeLAN       config.BoolOption `json:"-"`
	BlockScopeInternet  config.BoolOption `json:"-"`
	BlockP2P            config.BoolOption `json:"-"`
	BlockInbound        config.BoolOption `json:"-"`
	RemoveOutOfScopeDNS config.BoolOption `json:"-"`
	RemoveBlockedDNS    config.BoolOption `json:"-"`
	FilterSubDomains    config.BoolOption `json:"-"`
	FilterCNAMEs        config.BoolOption `json:"-"`
	PreventBypassing    config.BoolOption `json:"-"`
	DomainHeuristics    config.BoolOption `json:"-"`
	UseSPN              config.BoolOption `json:"-"`
}

// NewLayeredProfile returns a new layered profile based on the given local profile.
func NewLayeredProfile(localProfile *Profile) *LayeredProfile {
	var securityLevelVal uint32

	new := &LayeredProfile{
		localProfile:       localProfile,
		layers:             make([]*Profile, 0, len(localProfile.LinkedProfiles)+1),
		LayerIDs:           make([]string, 0, len(localProfile.LinkedProfiles)+1),
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
	new.DomainHeuristics = new.wrapSecurityLevelOption(
		CfgOptionDomainHeuristicsKey,
		cfgOptionDomainHeuristics,
	)
	new.UseSPN = new.wrapBoolOption(
		CfgOptionUseSPNKey,
		cfgOptionUseSPN,
	)

	new.LayerIDs = append(new.LayerIDs, localProfile.ScopedID())
	new.layers = append(new.layers, localProfile)

	// TODO: Load additional profiles.

	new.updateCaches()

	new.CreateMeta()
	new.SetKey(runtime.DefaultRegistry.DatabaseName() + ":" + revisionProviderPrefix + localProfile.ID)

	return new
}

// LockForUsage locks the layered profile, including all layers individually.
func (lp *LayeredProfile) LockForUsage() {
	lp.RLock()
	for _, layer := range lp.layers {
		layer.RLock()
	}
}

// UnlockForUsage unlocks the layered profile, including all layers individually.
func (lp *LayeredProfile) UnlockForUsage() {
	lp.RUnlock()
	for _, layer := range lp.layers {
		layer.RUnlock()
	}
}

// LocalProfile returns the local profile associated with this layered profile.
func (lp *LayeredProfile) LocalProfile() *Profile {
	lp.RLock()
	defer lp.RUnlock()

	return lp.localProfile
}

// RevisionCnt returns the current profile revision counter.
func (lp *LayeredProfile) RevisionCnt() (revisionCounter uint64) {
	if lp == nil {
		return 0
	}

	lp.RLock()
	defer lp.RUnlock()

	return lp.RevisionCounter
}

// MarkStillActive marks all the layers as still active.
func (lp *LayeredProfile) MarkStillActive() {
	if lp == nil {
		return
	}

	lp.RLock()
	defer lp.RUnlock()

	for _, layer := range lp.layers {
		layer.MarkStillActive()
	}
}

// NeedsUpdate checks for outdated profiles.
func (lp *LayeredProfile) NeedsUpdate() (outdated bool) {
	lp.RLock()
	defer lp.RUnlock()

	// Check global config state.
	if !lp.globalValidityFlag.IsValid() {
		return true
	}

	// Check config in layers.
	for _, layer := range lp.layers {
		if layer.outdated.IsSet() {
			return true
		}
	}

	return false
}

// Update checks for and replaces any outdated profiles.
func (lp *LayeredProfile) Update() (revisionCounter uint64) {
	lp.Lock()
	defer lp.Unlock()

	var changed bool
	for i, layer := range lp.layers {
		if layer.outdated.IsSet() {
			changed = true
			// update layer
			newLayer, _, err := GetProfile(layer.Source, layer.ID, layer.LinkedPath)
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
		// get global config validity flag
		lp.globalValidityFlag.Refresh()

		// update cached data fields
		lp.updateCaches()

		// bump revision counter
		lp.RevisionCounter++
	}

	return lp.RevisionCounter
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
}

// MarkUsed marks the localProfile as used.
func (lp *LayeredProfile) MarkUsed() {
	lp.localProfile.MarkUsed()
}

// SecurityLevel returns the highest security level of all layered profiles. This function is atomic and does not require any locking.
func (lp *LayeredProfile) SecurityLevel() uint8 {
	return uint8(atomic.LoadUint32(lp.securityLevel))
}

// DefaultAction returns the active default action ID. This functions requires the layered profile to be read locked.
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

// MatchEndpoint checks if the given endpoint matches an entry in any of the profiles. This functions requires the layered profile to be read locked.
func (lp *LayeredProfile) MatchEndpoint(ctx context.Context, entity *intel.Entity) (endpoints.EPResult, endpoints.Reason) {
	for _, layer := range lp.layers {
		if layer.endpoints.IsSet() {
			result, reason := layer.endpoints.Match(ctx, entity)
			if endpoints.IsDecision(result) {
				return result, reason
			}
		}
	}

	cfgLock.RLock()
	defer cfgLock.RUnlock()
	return cfgEndpoints.Match(ctx, entity)
}

// MatchServiceEndpoint checks if the given endpoint of an inbound connection matches an entry in any of the profiles. This functions requires the layered profile to be read locked.
func (lp *LayeredProfile) MatchServiceEndpoint(ctx context.Context, entity *intel.Entity) (endpoints.EPResult, endpoints.Reason) {
	entity.EnableReverseResolving()

	for _, layer := range lp.layers {
		if layer.serviceEndpoints.IsSet() {
			result, reason := layer.serviceEndpoints.Match(ctx, entity)
			if endpoints.IsDecision(result) {
				return result, reason
			}
		}
	}

	cfgLock.RLock()
	defer cfgLock.RUnlock()
	return cfgServiceEndpoints.Match(ctx, entity)
}

// MatchFilterLists matches the entity against the set of filter
// lists. This functions requires the layered profile to be read locked.
func (lp *LayeredProfile) MatchFilterLists(ctx context.Context, entity *intel.Entity) (endpoints.EPResult, endpoints.Reason) {
	entity.ResolveSubDomainLists(ctx, lp.FilterSubDomains())
	entity.EnableCNAMECheck(ctx, lp.FilterCNAMEs())

	for _, layer := range lp.layers {
		// search for the first layer that has filterListIDs set
		if len(layer.filterListIDs) > 0 {
			entity.LoadLists(ctx)

			if entity.MatchLists(layer.filterListIDs) {
				return endpoints.Denied, entity.ListBlockReason()
			}

			return endpoints.NoMatch, nil
		}
	}

	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if len(cfgFilterLists) > 0 {
		entity.LoadLists(ctx)

		if entity.MatchLists(cfgFilterLists) {
			return endpoints.Denied, entity.ListBlockReason()
		}
	}

	return endpoints.NoMatch, nil
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

func (lp *LayeredProfile) wrapBoolOption(configKey string, globalConfig config.BoolOption) config.BoolOption {
	revCnt := lp.RevisionCounter
	var value bool
	var refreshLock sync.Mutex

	return func() bool {
		refreshLock.Lock()
		defer refreshLock.Unlock()

		// Check if we need to refresh the value.
		if revCnt != lp.RevisionCounter {
			revCnt = lp.RevisionCounter

			// Go through all layers to find an active value.
			found := false
			for _, layer := range lp.layers {
				layerValue, ok := layer.configPerspective.GetAsBool(configKey)
				if ok {
					found = true
					value = layerValue
					break
				}
			}
			if !found {
				value = globalConfig()
			}
		}

		return value
	}
}

func (lp *LayeredProfile) wrapIntOption(configKey string, globalConfig config.IntOption) config.IntOption {
	revCnt := lp.RevisionCounter
	var value int64
	var refreshLock sync.Mutex

	return func() int64 {
		refreshLock.Lock()
		defer refreshLock.Unlock()

		// Check if we need to refresh the value.
		if revCnt != lp.RevisionCounter {
			revCnt = lp.RevisionCounter

			// Go through all layers to find an active value.
			found := false
			for _, layer := range lp.layers {
				layerValue, ok := layer.configPerspective.GetAsInt(configKey)
				if ok {
					found = true
					value = layerValue
					break
				}
			}
			if !found {
				value = globalConfig()
			}
		}

		return value
	}
}

// GetProfileSource returns the database key of the first profile in the
// layers that has the given configuration key set. If it returns an empty
// string, the global profile can be assumed to have been effective.
func (lp *LayeredProfile) GetProfileSource(configKey string) string {
	for _, layer := range lp.layers {
		if layer.configPerspective.Has(configKey) {
			return layer.Key()
		}
	}

	// Global Profile
	return ""
}

/*
For later:

func (lp *LayeredProfile) wrapStringOption(configKey string, globalConfig config.StringOption) config.StringOption {
	revCnt := lp.RevisionCounter
	var value string
	var refreshLock sync.Mutex

	return func() string {
		refreshLock.Lock()
		defer refreshLock.Unlock()

		// Check if we need to refresh the value.
		if revCnt != lp.RevisionCounter {
			revCnt = lp.RevisionCounter

			// Go through all layers to find an active value.
			found := false
			for _, layer := range lp.layers {
				layerValue, ok := layer.configPerspective.GetAsString(configKey)
				if ok {
					found = true
					value = layerValue
					break
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
