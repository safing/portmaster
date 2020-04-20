package profile

import (
	"github.com/safing/portbase/config"
	"github.com/safing/portmaster/status"
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

	CfgOptionFilterListKey = "filter/lists"
	cfgOptionFilterLists   config.StringArrayOption

	CfgOptionFilterSubDomainsKey = "filter/includeSubdomains"
	cfgOptionFilterSubDomains    config.IntOption // security level option

	CfgOptionFilterCNAMEKey = "filter/includeCNAMEs"
	cfgOptionFilterCNAME    config.IntOption // security level option

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

	CfgOptionPreventBypassingKey = "filter/preventBypassing"
	cfgOptionPreventBypassing    config.IntOption // security level option
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
		ExternalOptType: "string list",
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
		DefaultValue:    status.SecurityLevelsAll,
		ValidationRegex: "^(4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionDisableAutoPermit = config.Concurrent.GetAsInt(CfgOptionDisableAutoPermitKey, int64(status.SecurityLevelsAll))
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
		Name:        "Service Endpoint Filter List",
		Key:         CfgOptionServiceEndpointsKey,
		Description: "Filter incoming connections by matching the source endpoint. Network Scope restrictions and the inbound permission still apply. Also not that the implicit default action of this list is to always block.",
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
	cfgOptionServiceEndpoints = config.Concurrent.GetAsStringArray(CfgOptionServiceEndpointsKey, []string{})
	cfgStringArrayOptions[CfgOptionServiceEndpointsKey] = cfgOptionServiceEndpoints

	// Filter list IDs
	err = config.Register(&config.Option{
		Name:            "Filter List",
		Key:             CfgOptionFilterListKey,
		Description:     "Filter connections by matching the endpoint against configured filterlists",
		OptType:         config.OptTypeStringArray,
		DefaultValue:    []string{"TRAC", "MAL"},
		ExternalOptType: "filter list",
		ValidationRegex: `^[a-zA-Z0-9\-]+$`,
	})
	if err != nil {
		return err
	}
	cfgOptionFilterLists = config.Concurrent.GetAsStringArray(CfgOptionFilterListKey, []string{})
	cfgStringArrayOptions[CfgOptionFilterListKey] = cfgOptionFilterLists

	// Include CNAMEs
	err = config.Register(&config.Option{
		Name:            "Filter CNAMEs",
		Key:             CfgOptionFilterCNAMEKey,
		Description:     "Also filter requests where a CNAME would be blocked",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    status.SecurityLevelsAll,
		ValidationRegex: "^(7|6|4)$",
		ExpertiseLevel:  config.ExpertiseLevelExpert,
	})
	if err != nil {
		return err
	}
	cfgOptionFilterCNAME = config.Concurrent.GetAsInt(CfgOptionFilterCNAMEKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionFilterCNAMEKey] = cfgOptionFilterCNAME

	// Include subdomains
	err = config.Register(&config.Option{
		Name:            "Filter SubDomains",
		Key:             CfgOptionFilterSubDomainsKey,
		Description:     "Also filter sub-domains if a parent domain is blocked by a filter list",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    status.SecurityLevelOff,
		ValidationRegex: "^(0|4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionFilterSubDomains = config.Concurrent.GetAsInt(CfgOptionFilterSubDomainsKey, int64(status.SecurityLevelOff))
	cfgIntOptions[CfgOptionFilterSubDomainsKey] = cfgOptionFilterSubDomains

	// Block Scope Local
	err = config.Register(&config.Option{
		Name:            "Block Scope Local",
		Key:             CfgOptionBlockScopeLocalKey,
		Description:     "Block connections to your own device, ie. localhost.",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    status.SecurityLevelOff,
		ValidationRegex: "^(0|4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeLocal = config.Concurrent.GetAsInt(CfgOptionBlockScopeLocalKey, int64(status.SecurityLevelOff))
	cfgIntOptions[CfgOptionBlockScopeLocalKey] = cfgOptionBlockScopeLocal

	// Block Scope LAN
	err = config.Register(&config.Option{
		Name:            "Block Scope LAN",
		Key:             CfgOptionBlockScopeLANKey,
		Description:     "Block connections to the Local Area Network.",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    status.SecurityLevelOff,
		ValidationRegex: "^(0|4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeLAN = config.Concurrent.GetAsInt(CfgOptionBlockScopeLANKey, int64(status.SecurityLevelOff))
	cfgIntOptions[CfgOptionBlockScopeLANKey] = cfgOptionBlockScopeLAN

	// Block Scope Internet
	err = config.Register(&config.Option{
		Name:            "Block Scope Internet",
		Key:             CfgOptionBlockScopeInternetKey,
		Description:     "Block connections to the Internet.",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    status.SecurityLevelOff,
		ValidationRegex: "^(0|4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeInternet = config.Concurrent.GetAsInt(CfgOptionBlockScopeInternetKey, int64(status.SecurityLevelOff))
	cfgIntOptions[CfgOptionBlockScopeInternetKey] = cfgOptionBlockScopeInternet

	// Block Peer to Peer Connections
	err = config.Register(&config.Option{
		Name:            "Block Peer to Peer Connections",
		Key:             CfgOptionBlockP2PKey,
		Description:     "Block peer to peer connections. These are connections that are established directly to an IP address on the Internet without resolving a domain name via DNS first.",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    status.SecurityLevelsAll,
		ValidationRegex: "^(4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionBlockP2P = config.Concurrent.GetAsInt(CfgOptionBlockP2PKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionBlockP2PKey] = cfgOptionBlockP2P

	// Block Inbound Connections
	err = config.Register(&config.Option{
		Name:            "Block Inbound Connections",
		Key:             CfgOptionBlockInboundKey,
		Description:     "Block inbound connections to your device. This will usually only be the case if you are running a network service or are using peer to peer software.",
		OptType:         config.OptTypeInt,
		ExternalOptType: "security level",
		DefaultValue:    status.SecurityLevelsHighAndExtreme,
		ValidationRegex: "^(4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionBlockInbound = config.Concurrent.GetAsInt(CfgOptionBlockInboundKey, int64(status.SecurityLevelsHighAndExtreme))
	cfgIntOptions[CfgOptionBlockInboundKey] = cfgOptionBlockInbound

	// Enforce SPN
	err = config.Register(&config.Option{
		Name:            "Enforce SPN",
		Key:             CfgOptionEnforceSPNKey,
		Description:     "This setting enforces connections to be routed over the SPN. If this is not possible for any reason, connections will be blocked.",
		OptType:         config.OptTypeInt,
		ReleaseLevel:    config.ReleaseLevelExperimental,
		ExternalOptType: "security level",
		DefaultValue:    status.SecurityLevelOff,
		ValidationRegex: "^(0|4|6|7)$",
	})
	if err != nil {
		return err
	}
	cfgOptionEnforceSPN = config.Concurrent.GetAsInt(CfgOptionEnforceSPNKey, int64(status.SecurityLevelOff))
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
		DefaultValue:    status.SecurityLevelsAll,
		ValidationRegex: "^(7|6|4)$",
	})
	if err != nil {
		return err
	}
	cfgOptionRemoveOutOfScopeDNS = config.Concurrent.GetAsInt(CfgOptionRemoveOutOfScopeDNSKey, int64(status.SecurityLevelsAll))
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
		DefaultValue:    status.SecurityLevelsAll,
		ValidationRegex: "^(7|6|4)$",
	})
	if err != nil {
		return err
	}
	cfgOptionRemoveBlockedDNS = config.Concurrent.GetAsInt(CfgOptionRemoveBlockedDNSKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionRemoveBlockedDNSKey] = cfgOptionRemoveBlockedDNS

	err = config.Register(&config.Option{
		Name:            "Prevent Bypassing",
		Key:             CfgOptionPreventBypassingKey,
		Description:     "Prevent apps from bypassing the privacy filter: Firefox by disabling DNS-over-HTTPs",
		OptType:         config.OptTypeInt,
		ExpertiseLevel:  config.ExpertiseLevelUser,
		ReleaseLevel:    config.ReleaseLevelBeta,
		ExternalOptType: "security level",
		DefaultValue:    status.SecurityLevelsAll,
		ValidationRegex: "^(7|6|4)",
	})
	if err != nil {
		return err
	}
	cfgOptionPreventBypassing = config.Concurrent.GetAsInt((CfgOptionPreventBypassingKey), int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionPreventBypassingKey] = cfgOptionPreventBypassing

	return nil
}
