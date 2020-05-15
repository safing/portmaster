package endpoints

import (
	"strings"

	"github.com/safing/portmaster/network/netutils"

	"github.com/safing/portmaster/intel"
)

const (
	scopeLocalhost        = 1
	scopeLocalhostName    = "Localhost"
	scopeLocalhostMatcher = "localhost"

	scopeLAN        = 2
	scopeLANName    = "LAN"
	scopeLANMatcher = "lan"

	scopeInternet        = 4
	scopeInternetName    = "Internet"
	scopeInternetMatcher = "internet"
)

// EndpointScope matches network scopes.
type EndpointScope struct {
	EndpointBase

	scopes uint8
}

// Localhost
// LAN
// Internet

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointScope) Matches(entity *intel.Entity) (EPResult, Reason) {
	if entity.IP == nil {
		return Undeterminable, nil
	}

	classification := netutils.ClassifyIP(entity.IP)
	var scope uint8
	switch classification {
	case netutils.HostLocal:
		scope = scopeLocalhost
	case netutils.LinkLocal:
		scope = scopeLAN
	case netutils.SiteLocal:
		scope = scopeLAN
	case netutils.Global:
		scope = scopeInternet
	case netutils.LocalMulticast:
		scope = scopeLAN
	case netutils.GlobalMulticast:
		scope = scopeInternet
	}

	if ep.scopes&scope > 0 {
		return ep.match(ep, entity, ep.Scopes(), "scope matches")
	}
	return NoMatch, nil
}

// Scopes returns the string representation of all scopes.
func (ep *EndpointScope) Scopes() string {
	if ep.scopes == 3 || ep.scopes > 4 {
		// single scope
		switch ep.scopes {
		case scopeLocalhost:
			return scopeLocalhostName
		case scopeLAN:
			return scopeLANName
		case scopeInternet:
			return scopeInternetName
		}
	}

	// multiple scopes
	var s []string
	if ep.scopes&scopeLocalhost > 0 {
		s = append(s, scopeLocalhostName)
	}
	if ep.scopes&scopeLAN > 0 {
		s = append(s, scopeLANName)
	}
	if ep.scopes&scopeInternet > 0 {
		s = append(s, scopeInternetName)
	}
	return strings.Join(s, ",")
}

func (ep *EndpointScope) String() string {
	return ep.renderPPP(ep.Scopes())
}

func parseTypeScope(fields []string) (Endpoint, error) {
	ep := &EndpointScope{}
	for _, val := range strings.Split(strings.ToLower(fields[1]), ",") {
		switch val {
		case scopeLocalhostMatcher:
			ep.scopes &= scopeLocalhost
		case scopeLANMatcher:
			ep.scopes &= scopeLAN
		case scopeInternetMatcher:
			ep.scopes &= scopeInternet
		default:
			return nil, nil
		}
	}
	return ep.parsePPP(ep, fields)
}
