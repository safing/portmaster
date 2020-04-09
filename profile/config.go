package profile

import (
	"github.com/safing/portbase/config"
)

// Configuration Keys
var (
	cfgStringOptions      = make(map[string]config.StringOption)
	cfgStringArrayOptions = make(map[string]config.StringArrayOption)
	cfgIntOptions         = make(map[string]config.IntOption)
	cfgBoolOptions        = make(map[string]config.BoolOption)

	CfgOptionDefaultActionKey = "filter/defaultAction"
	cfgOptionDefaultAction    config.StringOption

	CfgOptionDisableAutoPermitKey = "filter/disableAutoPermit"
	cfgOptionDisableAutoPermit    config.IntOption // security level option

	CfgOptionEndpointsKey = "filter/endpoints"
	cfgOptionEndpoints    config.StringArrayOption

	CfgOptionServiceEndpointsKey = "filter/serviceEndpoints"
	cfgOptionServiceEndpoints    config.StringArrayOption

	CfgOptionBlockScopeLocalKey = "filter/blockLocal"
	cfgOptionBlockScopeLocal    config.IntOption // security level option

	CfgOptionBlockScopeLANKey = "filter/blockLAN"
	cfgOptionBlockScopeLAN    config.IntOption // security level option

	CfgOptionBlockScopeInternetKey = "filter/blockInternet"
	cfgOptionBlockScopeInternet    config.IntOption // security level option

	CfgOptionBlockP2PKey = "filter/blockP2P"
	cfgOptionBlockP2P    config.IntOption // security level option

	CfgOptionBlockInboundKey = "filter/blockInbound"
	cfgOptionBlockInbound    config.IntOption // security level option

	CfgOptionEnforceSPNKey = "filter/enforceSPN"
	cfgOptionEnforceSPN    config.IntOption // security level option

	CfgOptionRemoveOutOfScopeDNSKey = "filter/removeOutOfScopeDNS"
	cfgOptionRemoveOutOfScopeDNS    config.IntOption // security level option

	CfgOptionRemoveBlockedDNSKey = "filter/removeBlockedDNS"
	cfgOptionRemoveBlockedDNS    config.IntOption // security level option
)

func registerConfiguration() error {
	// Default Filter Action
	// permit - blacklist mode: everything is permitted unless blocked
	// ask - ask mode: if not verdict is found, the user is consulted
	// block - whitelist mode: everything is blocked unless permitted
	err := config.Register(&config.Option{
		Name:            "Default Filter Action",
		Key:             CfgOptionDefaultActionKey,
		Description:     `The default filter action when nothing else permits or blocks a connection.`,
		OptType:         config.OptTypeString,
		DefaultValue:    "permit",
		ValidationRegex: "^(permit|ask|block)$",
	})
	if err != nil {
		return err
	}
	cfgOptionDefaultAction = config.Concurrent.GetAsString(CfgOptionDefaultActionKey, "permit")
	cfgStringOptions[CfgOptionDefaultActionKey] = cfgOptionDefaultAction

	// Disable Auto Permit
	err = config.Register(&config.Option{
		Name:            "Disable Auto Permit",
		Key:             CfgOptionDisableAutoPermitKey,
		Description:     "Auto Permit searches for a relation between an app and the destionation of a connection - if there is a correlation, the connection will be permitted. This setting is negated in order to provide a streamlined user experience, where higher settings are better.",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    4,
		ValidationRegex: "^(4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionDisableAutoPermit = config.Concurrent.GetAsInt(CfgOptionDisableAutoPermitKey, 4)
	cfgIntOptions[CfgOptionDisableAutoPermitKey] = cfgOptionDisableAutoPermit

	// Endpoint Filter List
	err = config.Register(&config.Option{
		Name:        "Endpoint Filter List",
		Key:         CfgOptionEndpointsKey,
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
		DefaultValue:    []string{},
		ExternalOptType: "endpoint list",
		ValidationRegex: `^(\+|\-) [A-z0-9\.:\-*/]+( [A-z0-9/]+)?$`,
	})
	if err != nil {
		return err
	}
	cfgOptionEndpoints = config.Concurrent.GetAsStringArray(CfgOptionEndpointsKey, []string{})
	cfgStringArrayOptions[CfgOptionEndpointsKey] = cfgOptionEndpoints

	// Service Endpoint Filter List
	err = config.Register(&config.Option{
		Name:            "Service Endpoint Filter List",
		Key:             CfgOptionServiceEndpointsKey,
		Description:     "Filter incoming connections by matching the source endpoint. Network Scope restrictions and the inbound permission still apply. Also not that the implicit default action of this list is to always block.",
		OptType:         config.OptTypeStringArray,
		DefaultValue:    []string{},
		ExternalOptType: "endpoint list",
		ValidationRegex: `^(\+|\-) [A-z0-9\.:\-*/]+( [A-z0-9/]+)?$`,
	})
	if err != nil {
		return err
	}
	cfgOptionServiceEndpoints = config.Concurrent.GetAsStringArray(CfgOptionServiceEndpointsKey, []string{})
	cfgStringArrayOptions[CfgOptionServiceEndpointsKey] = cfgOptionServiceEndpoints

	// Block Scope Local
	err = config.Register(&config.Option{
		Name:            "Block Scope Local",
		Key:             CfgOptionBlockScopeLocalKey,
		Description:     "Block connections to your own device, ie. localhost.",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    0,
		ValidationRegex: "^(0|4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeLocal = config.Concurrent.GetAsInt(CfgOptionBlockScopeLocalKey, 0)
	cfgIntOptions[CfgOptionBlockScopeLocalKey] = cfgOptionBlockScopeLocal

	// Block Scope LAN
	err = config.Register(&config.Option{
		Name:            "Block Scope LAN",
		Key:             CfgOptionBlockScopeLANKey,
		Description:     "Block connections to the Local Area Network.",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    0,
		ValidationRegex: "^(0|4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeLAN = config.Concurrent.GetAsInt(CfgOptionBlockScopeLANKey, 0)
	cfgIntOptions[CfgOptionBlockScopeLANKey] = cfgOptionBlockScopeLAN

	// Block Scope Internet
	err = config.Register(&config.Option{
		Name:            "Block Scope Internet",
		Key:             CfgOptionBlockScopeInternetKey,
		Description:     "Block connections to the Internet.",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    0,
		ValidationRegex: "^(0|4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeInternet = config.Concurrent.GetAsInt(CfgOptionBlockScopeInternetKey, 0)
	cfgIntOptions[CfgOptionBlockScopeInternetKey] = cfgOptionBlockScopeInternet

	// Block Peer to Peer Connections
	err = config.Register(&config.Option{
		Name:            "Block Peer to Peer Connections",
		Key:             CfgOptionBlockP2PKey,
		Description:     "Block peer to peer connections. These are connections that are established directly to an IP address on the Internet without resolving a domain name via DNS first.",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    7,
		ValidationRegex: "^(4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionBlockP2P = config.Concurrent.GetAsInt(CfgOptionBlockP2PKey, 7)
	cfgIntOptions[CfgOptionBlockP2PKey] = cfgOptionBlockP2P

	// Block Inbound Connections
	err = config.Register(&config.Option{
		Name:            "Block Inbound Connections",
		Key:             CfgOptionBlockInboundKey,
		Description:     "Block inbound connections to your device. This will usually only be the case if you are running a network service or are using peer to peer software.",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    4,
		ValidationRegex: "^(4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionBlockInbound = config.Concurrent.GetAsInt(CfgOptionBlockInboundKey, 4)
	cfgIntOptions[CfgOptionBlockInboundKey] = cfgOptionBlockInbound

	// Enforce SPN
	err = config.Register(&config.Option{
		Name:            "Enforce SPN",
		Key:             CfgOptionEnforceSPNKey,
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
	cfgOptionEnforceSPN = config.Concurrent.GetAsInt(CfgOptionEnforceSPNKey, 0)
	cfgIntOptions[CfgOptionEnforceSPNKey] = cfgOptionEnforceSPN

	// Filter Out-of-Scope DNS Records
	err = config.Register(&config.Option{
		Name:            "Filter Out-of-Scope DNS Records",
		Key:             CfgOptionRemoveOutOfScopeDNSKey,
		Description:     "Filter DNS answers that are outside of the scope of the server. A server on the public Internet may not respond with a private LAN address.",
		OptType:         config.OptTypeInt,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelBeta,
		ExternalOptType: "security level",
		DefaultValue:    7,
		ValidationRegex: "^(7|6|4)$",
	})
	if err != nil {
		return err
	}
	cfgOptionRemoveOutOfScopeDNS = config.Concurrent.GetAsInt(CfgOptionRemoveOutOfScopeDNSKey, 7)
	cfgIntOptions[CfgOptionRemoveOutOfScopeDNSKey] = cfgOptionRemoveOutOfScopeDNS

	// Filter DNS Records that would be blocked
	err = config.Register(&config.Option{
		Name:            "Filter DNS Records that would be blocked",
		Key:             CfgOptionRemoveBlockedDNSKey,
		Description:     "Pre-filter DNS answers that an application would not be allowed to connect to.",
		OptType:         config.OptTypeInt,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelBeta,
		ExternalOptType: "security level",
		DefaultValue:    7,
		ValidationRegex: "^(7|6|4)$",
	})
	if err != nil {
		return err
	}
	cfgOptionRemoveBlockedDNS = config.Concurrent.GetAsInt(CfgOptionRemoveBlockedDNSKey, 7)
	cfgIntOptions[CfgOptionRemoveBlockedDNSKey] = cfgOptionRemoveBlockedDNS

	return nil
}
