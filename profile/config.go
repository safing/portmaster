package profile

import (
	"github.com/safing/portbase/config"
)

var (
	cfgStringOptions      = make(map[string]config.StringOption)
	cfgStringArrayOptions = make(map[string]config.StringArrayOption)
	cfgIntOptions         = make(map[string]config.IntOption)
	cfgBoolOptions        = make(map[string]config.BoolOption)

	cfgOptionDefaultActionKey = "filter/mode"
	cfgOptionDefaultAction    config.StringOption

	cfgOptionDisableAutoPermitKey = "filter/disableAutoPermit"
	cfgOptionDisableAutoPermit    config.IntOption // security level option

	cfgOptionEndpointsKey = "filter/endpoints"
	cfgOptionEndpoints    config.StringArrayOption

	cfgOptionServiceEndpointsKey = "filter/serviceEndpoints"
	cfgOptionServiceEndpoints    config.StringArrayOption

	cfgOptionBlockScopeLocalKey = "filter/blockLocal"
	cfgOptionBlockScopeLocal    config.IntOption // security level option

	cfgOptionBlockScopeLANKey = "filter/blockLAN"
	cfgOptionBlockScopeLAN    config.IntOption // security level option

	cfgOptionBlockScopeInternetKey = "filter/blockInternet"
	cfgOptionBlockScopeInternet    config.IntOption // security level option

	cfgOptionBlockP2PKey = "filter/blockP2P"
	cfgOptionBlockP2P    config.IntOption // security level option

	cfgOptionBlockInboundKey = "filter/blockInbound"
	cfgOptionBlockInbound    config.IntOption // security level option

	cfgOptionEnforceSPNKey = "filter/enforceSPN"
	cfgOptionEnforceSPN    config.IntOption // security level option
)

func registerConfiguration() error {
	// Default Filter Action
	// permit - blacklist mode: everything is permitted unless blocked
	// ask - ask mode: if not verdict is found, the user is consulted
	// block - whitelist mode: everything is blocked unless permitted
	err := config.Register(&config.Option{
		Name:            "Default Filter Action",
		Key:             cfgOptionDefaultActionKey,
		Description:     `The default filter action when nothing else permits or blocks a connection.`,
		OptType:         config.OptTypeString,
		DefaultValue:    "permit",
		ValidationRegex: "^(permit|ask|block)$",
	})
	if err != nil {
		return err
	}
	cfgOptionDefaultAction = config.Concurrent.GetAsString(cfgOptionDefaultActionKey, "permit")
	cfgStringOptions[cfgOptionDefaultActionKey] = cfgOptionDefaultAction

	// Disable Auto Permit
	err = config.Register(&config.Option{
		Name:            "Disable Auto Permit",
		Key:             cfgOptionDisableAutoPermitKey,
		Description:     "Auto Permit searches for a relation between an app and the destionation of a connection - if there is a correlation, the connection will be permitted. This setting is negated in order to provide a streamlined user experience, where higher settings are better.",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    4,
		ValidationRegex: "^(4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionDisableAutoPermit = config.Concurrent.GetAsInt(cfgOptionDisableAutoPermitKey, 4)
	cfgIntOptions[cfgOptionDisableAutoPermitKey] = cfgOptionDisableAutoPermit

	// Endpoint Filter List
	err = config.Register(&config.Option{
		Name:        "Endpoint Filter List",
		Key:         cfgOptionEndpointsKey,
		Description: "Filter outgoing connections by matching the destination endpoint. Network Scope restrictions still apply.",
		Help: `Format:
	Permission:
		"+": permit
		"-": block
	Host Matching:
		IP, CIDR, Country Code, ASN, "*" for any
		Domains:
			"example.com": exact match
			".example.com": exact match + subdomains
			"*xample.com": prefix wildcard
			"example.*": suffix wildcard
			"*example*": prefix and suffix wildcard  
	Protocol and Port Matching (optional):
		<protocol>/<port>

Examples:
	+ .example.com */HTTP
	- .example.com
	+ 192.168.0.1/24`,
		OptType:         config.OptTypeStringArray,
		DefaultValue:    nil,
		ExternalOptType: "endpoint list",
		ValidationRegex: `^(+|-) [A-z0-9\.:-*/]+( [A-z0-9/]+)?$`,
	})
	if err != nil {
		return err
	}
	cfgOptionEndpoints = config.Concurrent.GetAsStringArray(cfgOptionEndpointsKey, nil)
	cfgStringArrayOptions[cfgOptionEndpointsKey] = cfgOptionEndpoints

	// Service Endpoint Filter List
	err = config.Register(&config.Option{
		Name:            "Service Endpoint Filter List",
		Key:             cfgOptionServiceEndpointsKey,
		Description:     "Filter incoming connections by matching the source endpoint. Network Scope restrictions and the inbound permission still apply. Also not that the implicit default action of this list is to always block.",
		OptType:         config.OptTypeStringArray,
		DefaultValue:    nil,
		ExternalOptType: "endpoint list",
		ValidationRegex: `^(+|-) [A-z0-9\.:-*/]+( [A-z0-9/]+)?$`,
	})
	if err != nil {
		return err
	}
	cfgOptionServiceEndpoints = config.Concurrent.GetAsStringArray(cfgOptionServiceEndpointsKey, nil)
	cfgStringArrayOptions[cfgOptionServiceEndpointsKey] = cfgOptionServiceEndpoints

	// Block Scope Local
	err = config.Register(&config.Option{
		Name:            "Block Scope Local",
		Key:             cfgOptionBlockScopeLocalKey,
		Description:     "Block connections to your own device, ie. localhost.",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    0,
		ValidationRegex: "^(0|4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeLocal = config.Concurrent.GetAsInt(cfgOptionBlockScopeLocalKey, 0)
	cfgIntOptions[cfgOptionBlockScopeLocalKey] = cfgOptionBlockScopeLocal

	// Block Scope LAN
	err = config.Register(&config.Option{
		Name:            "Block Scope LAN",
		Key:             cfgOptionBlockScopeLANKey,
		Description:     "Block connections to the Local Area Network.",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    0,
		ValidationRegex: "^(0|4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeLAN = config.Concurrent.GetAsInt(cfgOptionBlockScopeLANKey, 0)
	cfgIntOptions[cfgOptionBlockScopeLANKey] = cfgOptionBlockScopeLAN

	// Block Scope Internet
	err = config.Register(&config.Option{
		Name:            "Block Scope Internet",
		Key:             cfgOptionBlockScopeInternetKey,
		Description:     "Block connections to the Internet.",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    0,
		ValidationRegex: "^(0|4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeInternet = config.Concurrent.GetAsInt(cfgOptionBlockScopeInternetKey, 0)
	cfgIntOptions[cfgOptionBlockScopeInternetKey] = cfgOptionBlockScopeInternet

	// Block Peer to Peer Connections
	err = config.Register(&config.Option{
		Name:            "Block Peer to Peer Connections",
		Key:             cfgOptionBlockP2PKey,
		Description:     "Block peer to peer connections. These are connections that are established directly to an IP address on the Internet without resolving a domain name via DNS first.",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    7,
		ValidationRegex: "^(4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionBlockP2P = config.Concurrent.GetAsInt(cfgOptionBlockP2PKey, 7)
	cfgIntOptions[cfgOptionBlockP2PKey] = cfgOptionBlockP2P

	// Block Inbound Connections
	err = config.Register(&config.Option{
		Name:            "Block Inbound Connections",
		Key:             cfgOptionBlockInboundKey,
		Description:     "Block inbound connections to your device. This will usually only be the case if you are running a network service or are using peer to peer software.",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    4,
		ValidationRegex: "^(4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionBlockInbound = config.Concurrent.GetAsInt(cfgOptionBlockInboundKey, 6)
	cfgIntOptions[cfgOptionBlockInboundKey] = cfgOptionBlockInbound

	// Enforce SPN
	err = config.Register(&config.Option{
		Name:            "Enforce SPN",
		Key:             cfgOptionEnforceSPNKey,
		Description:     "This setting enforces connections to be routed over the SPN. If this is not possible for any reason, connections will be blocked.",
		OptType:         config.OptTypeInt,
		ReleaseLevel:    config.ReleaseLevelExperimental,
		ExternalOptType: "security level",
		DefaultValue:    0,
		ValidationRegex: "^(0|4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionEnforceSPN = config.Concurrent.GetAsInt(cfgOptionEnforceSPNKey, 0)
	cfgIntOptions[cfgOptionEnforceSPNKey] = cfgOptionEnforceSPN

	return nil
}
