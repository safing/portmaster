package endpoints

import (
	"context"
	"strings"

	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/network/netutils"
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

// Matches checks whether the given entity matches this endpoint definition.
func (ep *EndpointScope) Matches(_ context.Context, entity *intel.Entity) (EPResult, Reason) {
	if entity.IP == nil {
		return NoMatch, nil
	}

	var scope uint8
	switch entity.IPScope {
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
	case netutils.Undefined, netutils.Invalid:
		return NoMatch, nil
	}

	if ep.scopes&scope > 0 {
		return ep.match(ep, entity, ep.Scopes(), "scope matches")
	}
	return NoMatch, nil
}

// Scopes returns the string representation of all scopes.
func (ep *EndpointScope) Scopes() string {
	// single scope
	switch ep.scopes {
	case scopeLocalhost:
		return scopeLocalhostName
	case scopeLAN:
		return scopeLANName
	case scopeInternet:
		return scopeInternetName
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
			ep.scopes ^= scopeLocalhost
		case scopeLANMatcher:
			ep.scopes ^= scopeLAN
		case scopeInternetMatcher:
			ep.scopes ^= scopeInternet
		default:
			return nil, nil
		}
	}
	return ep.parsePPP(ep, fields)
}
