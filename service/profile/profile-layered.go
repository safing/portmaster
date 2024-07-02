package profile

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/runtime"
	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/profile/endpoints"
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

	DisableAutoPermit   config.BoolOption   `json:"-"`
	BlockScopeLocal     config.BoolOption   `json:"-"`
	BlockScopeLAN       config.BoolOption   `json:"-"`
	BlockScopeInternet  config.BoolOption   `json:"-"`
	BlockP2P            config.BoolOption   `json:"-"`
	BlockInbound        config.BoolOption   `json:"-"`
	RemoveOutOfScopeDNS config.BoolOption   `json:"-"`
	RemoveBlockedDNS    config.BoolOption   `json:"-"`
	FilterSubDomains    config.BoolOption   `json:"-"`
	FilterCNAMEs        config.BoolOption   `json:"-"`
	PreventBypassing    config.BoolOption   `json:"-"`
	DomainHeuristics    config.BoolOption   `json:"-"`
	UseSPN              config.BoolOption   `json:"-"`
	SPNRoutingAlgorithm config.StringOption `json:"-"`
	EnableHistory       config.BoolOption   `json:"-"`
	KeepHistory         config.IntOption    `json:"-"`
}

// NewLayeredProfile returns a new layered profile based on the given local profile.
func NewLayeredProfile(localProfile *Profile) *LayeredProfile {
	var securityLevelVal uint32

	lp := &LayeredProfile{
		localProfile:       localProfile,
		layers:             make([]*Profile, 0, 1),
		LayerIDs:           make([]string, 0, 1),
		globalValidityFlag: config.NewValidityFlag(),
		RevisionCounter:    1,
		securityLevel:      &securityLevelVal,
	}

	lp.DisableAutoPermit = lp.wrapBoolOption(
		CfgOptionDisableAutoPermitKey,
		cfgOptionDisableAutoPermit,
	)
	lp.BlockScopeLocal = lp.wrapBoolOption(
		CfgOptionBlockScopeLocalKey,
		cfgOptionBlockScopeLocal,
	)
	lp.BlockScopeLAN = lp.wrapBoolOption(
		CfgOptionBlockScopeLANKey,
		cfgOptionBlockScopeLAN,
	)
	lp.BlockScopeInternet = lp.wrapBoolOption(
		CfgOptionBlockScopeInternetKey,
		cfgOptionBlockScopeInternet,
	)
	lp.BlockP2P = lp.wrapBoolOption(
		CfgOptionBlockP2PKey,
		cfgOptionBlockP2P,
	)
	lp.BlockInbound = lp.wrapBoolOption(
		CfgOptionBlockInboundKey,
		cfgOptionBlockInbound,
	)
	lp.RemoveOutOfScopeDNS = lp.wrapBoolOption(
		CfgOptionRemoveOutOfScopeDNSKey,
		cfgOptionRemoveOutOfScopeDNS,
	)
	lp.RemoveBlockedDNS = lp.wrapBoolOption(
		CfgOptionRemoveBlockedDNSKey,
		cfgOptionRemoveBlockedDNS,
	)
	lp.FilterSubDomains = lp.wrapBoolOption(
		CfgOptionFilterSubDomainsKey,
		cfgOptionFilterSubDomains,
	)
	lp.FilterCNAMEs = lp.wrapBoolOption(
		CfgOptionFilterCNAMEKey,
		cfgOptionFilterCNAME,
	)
	lp.PreventBypassing = lp.wrapBoolOption(
		CfgOptionPreventBypassingKey,
		cfgOptionPreventBypassing,
	)
	lp.DomainHeuristics = lp.wrapBoolOption(
		CfgOptionDomainHeuristicsKey,
		cfgOptionDomainHeuristics,
	)
	lp.UseSPN = lp.wrapBoolOption(
		CfgOptionUseSPNKey,
		cfgOptionUseSPN,
	)
	lp.SPNRoutingAlgorithm = lp.wrapStringOption(
		CfgOptionRoutingAlgorithmKey,
		cfgOptionRoutingAlgorithm,
	)
	lp.EnableHistory = lp.wrapBoolOption(
		CfgOptionEnableHistoryKey,
		cfgOptionEnableHistory,
	)
	lp.KeepHistory = lp.wrapIntOption(
		CfgOptionKeepHistoryKey,
		cfgOptionKeepHistory,
	)

	lp.LayerIDs = append(lp.LayerIDs, localProfile.ScopedID())
	lp.layers = append(lp.layers, localProfile)

	// TODO: Load additional profiles.

	lp.CreateMeta()
	lp.SetKey(runtime.DefaultRegistry.DatabaseName() + ":" + revisionProviderPrefix + localProfile.ScopedID())

	// Inform database subscribers about the new layered profile.
	lp.Lock()
	defer lp.Unlock()

	pushLayeredProfile(lp)

	return lp
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
	if lp == nil {
		return nil
	}

	lp.RLock()
	defer lp.RUnlock()

	return lp.localProfile
}

// LocalProfileWithoutLocking returns the local profile associated with this
// layered profile, but without locking the layered profile.
// This method my only be used when the caller already has a lock on the layered profile.
func (lp *LayeredProfile) LocalProfileWithoutLocking() *Profile {
	if lp == nil {
		return nil
	}

	return lp.localProfile
}

// increaseRevisionCounter increases the revision counter and pushes the
// layered profile to listeners.
func (lp *LayeredProfile) increaseRevisionCounter(lock bool) (revisionCounter uint64) { //nolint:unparam // This is documentation.
	if lp == nil {
		return 0
	}

	if lock {
		lp.Lock()
		defer lp.Unlock()
	}

	// Increase the revision counter.
	lp.RevisionCounter++
	// Push the increased counter to the UI.
	pushLayeredProfile(lp)

	return lp.RevisionCounter
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
func (lp *LayeredProfile) Update(md MatchingData, createProfileCallback func() *Profile) (revisionCounter uint64) {
	lp.Lock()
	defer lp.Unlock()

	var changed bool
	for i, layer := range lp.layers {
		if layer.outdated.IsSet() {
			// Check for unsupported sources.
			if layer.Source != SourceLocal {
				log.Warningf("profile: updating profiles outside of local source is not supported: %s", layer.ScopedID())
				layer.outdated.UnSet()
				continue
			}

			// Update layer.
			changed = true
			newLayer, err := GetLocalProfile(layer.ID, md, createProfileCallback)
			if err != nil {
				log.Errorf("profiles: failed to update profile %s: %s", layer.ScopedID(), err)
			} else {
				lp.layers[i] = newLayer
			}

			// Update local profile reference.
			if i == 0 {
				lp.localProfile = newLayer
			}
		}
	}
	if !lp.globalValidityFlag.IsValid() {
		changed = true
	}

	if changed {
		// get global config validity flag
		lp.globalValidityFlag.Refresh()

		// bump revision counter
		lp.increaseRevisionCounter(false)
	}

	return lp.RevisionCounter
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

// MatchSPNUsagePolicy checks if the given endpoint matches an entry in any of the profiles. This functions requires the layered profile to be read locked.
func (lp *LayeredProfile) MatchSPNUsagePolicy(ctx context.Context, entity *intel.Entity) (endpoints.EPResult, endpoints.Reason) {
	for _, layer := range lp.layers {
		if layer.spnUsagePolicy.IsSet() {
			result, reason := layer.spnUsagePolicy.Match(ctx, entity)
			if endpoints.IsDecision(result) {
				return result, reason
			}
		}
	}

	cfgLock.RLock()
	defer cfgLock.RUnlock()
	return cfgSPNUsagePolicy.Match(ctx, entity)
}

// StackedTransitHubPolicies returns all transit hub policies of the layered profile, including the global one.
func (lp *LayeredProfile) StackedTransitHubPolicies() []endpoints.Endpoints {
	policies := make([]endpoints.Endpoints, 0, len(lp.layers)+3) // +1 for global policy, +2 for intel policies

	for _, layer := range lp.layers {
		if layer.spnTransitHubPolicy.IsSet() {
			policies = append(policies, layer.spnTransitHubPolicy)
		}
	}

	cfgLock.RLock()
	defer cfgLock.RUnlock()
	policies = append(policies, cfgSPNTransitHubPolicy)

	return policies
}

// StackedExitHubPolicies returns all exit hub policies of the layered profile, including the global one.
func (lp *LayeredProfile) StackedExitHubPolicies() []endpoints.Endpoints {
	policies := make([]endpoints.Endpoints, 0, len(lp.layers)+3) // +1 for global policy, +2 for intel policies

	for _, layer := range lp.layers {
		if layer.spnExitHubPolicy.IsSet() {
			policies = append(policies, layer.spnExitHubPolicy)
		}
	}

	cfgLock.RLock()
	defer cfgLock.RUnlock()
	policies = append(policies, cfgSPNExitHubPolicy)

	return policies
}

// MatchFilterLists matches the entity against the set of filter
// lists. This functions requires the layered profile to be read locked.
func (lp *LayeredProfile) MatchFilterLists(ctx context.Context, entity *intel.Entity) (endpoints.EPResult, endpoints.Reason) {
	entity.ResolveSubDomainLists(ctx, lp.FilterSubDomains())
	entity.EnableCNAMECheck(ctx, lp.FilterCNAMEs())

	for _, layer := range lp.layers {
		// Search for the first layer that has filter lists set.
		if layer.filterListsSet {
			if entity.MatchLists(layer.filterListIDs) {
				return endpoints.Denied, entity.ListBlockReason()
			}

			return endpoints.NoMatch, nil
		}
	}

	cfgLock.RLock()
	defer cfgLock.RUnlock()
	if len(cfgFilterLists) > 0 {
		if entity.MatchLists(cfgFilterLists) {
			return endpoints.Denied, entity.ListBlockReason()
		}
	}

	return endpoints.NoMatch, nil
}

func (lp *LayeredProfile) wrapBoolOption(configKey string, globalConfig config.BoolOption) config.BoolOption {
	var revCnt uint64 = 0
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
	var revCnt uint64 = 0
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

func (lp *LayeredProfile) wrapStringOption(configKey string, globalConfig config.StringOption) config.StringOption {
	var revCnt uint64 = 0
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
