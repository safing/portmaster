package profile

import (
	"github.com/safing/portbase/config"
	"github.com/safing/portmaster/profile/endpoints"
	"github.com/safing/portmaster/status"
)

// Configuration Keys
var (
	cfgStringOptions      = make(map[string]config.StringOption)
	cfgStringArrayOptions = make(map[string]config.StringArrayOption)
	cfgIntOptions         = make(map[string]config.IntOption)
	cfgBoolOptions        = make(map[string]config.BoolOption)

	// Enable Filter Order = 0

	CfgOptionDefaultActionKey   = "filter/defaultAction"
	cfgOptionDefaultAction      config.StringOption
	cfgOptionDefaultActionOrder = 1

	// Prompt Timeout Order = 2

	CfgOptionBlockScopeInternetKey   = "filter/blockInternet"
	cfgOptionBlockScopeInternet      config.IntOption // security level option
	cfgOptionBlockScopeInternetOrder = 16

	CfgOptionBlockScopeLANKey   = "filter/blockLAN"
	cfgOptionBlockScopeLAN      config.IntOption // security level option
	cfgOptionBlockScopeLANOrder = 17

	CfgOptionBlockScopeLocalKey   = "filter/blockLocal"
	cfgOptionBlockScopeLocal      config.IntOption // security level option
	cfgOptionBlockScopeLocalOrder = 18

	CfgOptionBlockP2PKey   = "filter/blockP2P"
	cfgOptionBlockP2P      config.IntOption // security level option
	cfgOptionBlockP2POrder = 19

	CfgOptionBlockInboundKey   = "filter/blockInbound"
	cfgOptionBlockInbound      config.IntOption // security level option
	cfgOptionBlockInboundOrder = 20

	CfgOptionEndpointsKey   = "filter/endpoints"
	cfgOptionEndpoints      config.StringArrayOption
	cfgOptionEndpointsOrder = 32

	CfgOptionServiceEndpointsKey   = "filter/serviceEndpoints"
	cfgOptionServiceEndpoints      config.StringArrayOption
	cfgOptionServiceEndpointsOrder = 33

	CfgOptionPreventBypassingKey   = "filter/preventBypassing"
	cfgOptionPreventBypassing      config.IntOption // security level option
	cfgOptionPreventBypassingOrder = 48

	CfgOptionFilterListsKey   = "filter/lists"
	cfgOptionFilterLists      config.StringArrayOption
	cfgOptionFilterListsOrder = 64

	CfgOptionFilterSubDomainsKey   = "filter/includeSubdomains"
	cfgOptionFilterSubDomains      config.IntOption // security level option
	cfgOptionFilterSubDomainsOrder = 65

	CfgOptionFilterCNAMEKey   = "filter/includeCNAMEs"
	cfgOptionFilterCNAME      config.IntOption // security level option
	cfgOptionFilterCNAMEOrder = 66

	CfgOptionDisableAutoPermitKey   = "filter/disableAutoPermit"
	cfgOptionDisableAutoPermit      config.IntOption // security level option
	cfgOptionDisableAutoPermitOrder = 80

	CfgOptionEnforceSPNKey   = "filter/enforceSPN"
	cfgOptionEnforceSPN      config.IntOption // security level option
	cfgOptionEnforceSPNOrder = 96

	CfgOptionRemoveOutOfScopeDNSKey   = "filter/removeOutOfScopeDNS"
	cfgOptionRemoveOutOfScopeDNS      config.IntOption // security level option
	cfgOptionRemoveOutOfScopeDNSOrder = 112

	CfgOptionRemoveBlockedDNSKey   = "filter/removeBlockedDNS"
	cfgOptionRemoveBlockedDNS      config.IntOption // security level option
	cfgOptionRemoveBlockedDNSOrder = 113

	CfgOptionDomainHeuristicsKey   = "filter/domainHeuristics"
	cfgOptionDomainHeuristics      config.IntOption // security level option
	cfgOptionDomainHeuristicsOrder = 114

	// Permanent Verdicts Order = 128
)

func registerConfiguration() error {
	// Default Filter Action
	// permit - blocklist mode: everything is permitted unless blocked
	// ask - ask mode: if not verdict is found, the user is consulted
	// block - allowlist mode: everything is blocked unless permitted
	err := config.Register(&config.Option{
		Name:         "Default Filter Action",
		Key:          CfgOptionDefaultActionKey,
		Description:  `The default filter action when nothing else permits or blocks a connection.`,
		OptType:      config.OptTypeString,
		ReleaseLevel: config.ReleaseLevelExperimental,
		DefaultValue: "permit",
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  config.DisplayHintOneOf,
			config.DisplayOrderAnnotation: cfgOptionDefaultActionOrder,
			config.CategoryAnnotation:     "General",
		},
		PossibleValues: []config.PossibleValue{
			{
				Name:        "Permit",
				Value:       "permit",
				Description: "Permit all connections",
			},
			{
				Name:        "Ask",
				Value:       "ask",
				Description: "Always ask for a decision",
			},
			{
				Name:        "Block",
				Value:       "block",
				Description: "Block all connections",
			},
		},
	})
	if err != nil {
		return err
	}
	cfgOptionDefaultAction = config.Concurrent.GetAsString(CfgOptionDefaultActionKey, "permit")
	cfgStringOptions[CfgOptionDefaultActionKey] = cfgOptionDefaultAction

	// Disable Auto Permit
	err = config.Register(&config.Option{
		Name:         "Disable Auto Permit",
		Key:          CfgOptionDisableAutoPermitKey,
		Description:  "Auto Permit searches for a relation between an app and the destionation of a connection - if there is a correlation, the connection will be permitted. This setting is negated in order to provide a streamlined user experience, where higher settings are better.",
		OptType:      config.OptTypeInt,
		DefaultValue: status.SecurityLevelsAll,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionDisableAutoPermitOrder,
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.CategoryAnnotation: "Advanced",
		},
		PossibleValues: status.SecurityLevelValues,
	})
	if err != nil {
		return err
	}
	cfgOptionDisableAutoPermit = config.Concurrent.GetAsInt(CfgOptionDisableAutoPermitKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionDisableAutoPermitKey] = cfgOptionDisableAutoPermit

	filterListHelp := `Format:
	Permission:
		"+": permit
		"-": block
	Host Matching:
		IP, CIDR, Country Code, ASN, Filterlist, Network Scope, "*" for any
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
	+ 192.168.0.1
	+ 192.168.1.1/24
	+ Localhost,LAN
	- AS123456789
	- L:MAL
	+ AT
	- *`

	// Endpoint Filter List
	err = config.Register(&config.Option{
		Name:         "Outgoing Rules",
		Key:          CfgOptionEndpointsKey,
		Description:  "Rules that apply to outgoing network connections. Network Scope restrictions still apply.",
		Help:         filterListHelp,
		OptType:      config.OptTypeStringArray,
		DefaultValue: []string{},
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  endpoints.DisplayHintEndpointList,
			config.DisplayOrderAnnotation: cfgOptionEndpointsOrder,
			config.CategoryAnnotation:     "Rules",
		},
		ValidationRegex: `^(\+|\-) [A-z0-9\.:\-*/]+( [A-z0-9/]+)?$`,
	})
	if err != nil {
		return err
	}
	cfgOptionEndpoints = config.Concurrent.GetAsStringArray(CfgOptionEndpointsKey, []string{})
	cfgStringArrayOptions[CfgOptionEndpointsKey] = cfgOptionEndpoints

	// Service Endpoint Filter List
	err = config.Register(&config.Option{
		Name:         "Incoming Rules",
		Key:          CfgOptionServiceEndpointsKey,
		Description:  "Rules that apply to incoming network connections. Network Scope restrictions and the inbound permission still apply. Also not that the implicit default action of this list is to always block.",
		Help:         filterListHelp,
		OptType:      config.OptTypeStringArray,
		DefaultValue: []string{"+ Localhost"},
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  endpoints.DisplayHintEndpointList,
			config.DisplayOrderAnnotation: cfgOptionServiceEndpointsOrder,
			config.CategoryAnnotation:     "Rules",
		},
		ValidationRegex: `^(\+|\-) [A-z0-9\.:\-*/]+( [A-z0-9/]+)?$`,
	})
	if err != nil {
		return err
	}
	cfgOptionServiceEndpoints = config.Concurrent.GetAsStringArray(CfgOptionServiceEndpointsKey, []string{})
	cfgStringArrayOptions[CfgOptionServiceEndpointsKey] = cfgOptionServiceEndpoints

	// Filter list IDs
	err = config.Register(&config.Option{
		Name:         "Filter List",
		Key:          CfgOptionFilterListsKey,
		Description:  "Filter connections by matching the endpoint against configured filterlists",
		OptType:      config.OptTypeStringArray,
		DefaultValue: []string{"TRAC", "MAL"},
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  "filter list",
			config.DisplayOrderAnnotation: cfgOptionFilterListsOrder,
			config.CategoryAnnotation:     "Rules",
		},
		ValidationRegex: `^[a-zA-Z0-9\-]+$`,
	})
	if err != nil {
		return err
	}
	cfgOptionFilterLists = config.Concurrent.GetAsStringArray(CfgOptionFilterListsKey, []string{})
	cfgStringArrayOptions[CfgOptionFilterListsKey] = cfgOptionFilterLists

	// Include CNAMEs
	err = config.Register(&config.Option{
		Name:           "Filter CNAMEs",
		Key:            CfgOptionFilterCNAMEKey,
		Description:    "Also filter requests where a CNAME would be blocked",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelsAll,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionFilterCNAMEOrder,
			config.CategoryAnnotation: "DNS",
		},
		PossibleValues: status.SecurityLevelValues,
	})
	if err != nil {
		return err
	}
	cfgOptionFilterCNAME = config.Concurrent.GetAsInt(CfgOptionFilterCNAMEKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionFilterCNAMEKey] = cfgOptionFilterCNAME

	// Include subdomains
	err = config.Register(&config.Option{
		Name:           "Filter Subdomains",
		Key:            CfgOptionFilterSubDomainsKey,
		Description:    "Also filter a domain if any parent domain is blocked by a filter list",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelsAll,
		PossibleValues: status.SecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionFilterSubDomainsOrder,
			config.CategoryAnnotation: "DNS",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionFilterSubDomains = config.Concurrent.GetAsInt(CfgOptionFilterSubDomainsKey, int64(status.SecurityLevelOff))
	cfgIntOptions[CfgOptionFilterSubDomainsKey] = cfgOptionFilterSubDomains

	// Block Scope Local
	err = config.Register(&config.Option{
		Name:           "Block Scope Local",
		Key:            CfgOptionBlockScopeLocalKey,
		Description:    "Block internal connections on your own device, ie. localhost.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		DefaultValue:   status.SecurityLevelOff,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionBlockScopeLocalOrder,
			config.CategoryAnnotation:     "Scopes & Types",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeLocal = config.Concurrent.GetAsInt(CfgOptionBlockScopeLocalKey, int64(status.SecurityLevelOff))
	cfgIntOptions[CfgOptionBlockScopeLocalKey] = cfgOptionBlockScopeLocal

	// Block Scope LAN
	err = config.Register(&config.Option{
		Name:           "Block Scope LAN",
		Key:            CfgOptionBlockScopeLANKey,
		Description:    "Block connections to the Local Area Network.",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelsHighAndExtreme,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionBlockScopeLANOrder,
			config.CategoryAnnotation:     "Scopes & Types",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeLAN = config.Concurrent.GetAsInt(CfgOptionBlockScopeLANKey, int64(status.SecurityLevelOff))
	cfgIntOptions[CfgOptionBlockScopeLANKey] = cfgOptionBlockScopeLAN

	// Block Scope Internet
	err = config.Register(&config.Option{
		Name:           "Block Scope Internet",
		Key:            CfgOptionBlockScopeInternetKey,
		Description:    "Block connections to the Internet.",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelOff,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionBlockScopeInternetOrder,
			config.CategoryAnnotation:     "Scopes & Types",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeInternet = config.Concurrent.GetAsInt(CfgOptionBlockScopeInternetKey, int64(status.SecurityLevelOff))
	cfgIntOptions[CfgOptionBlockScopeInternetKey] = cfgOptionBlockScopeInternet

	// Block Peer to Peer Connections
	err = config.Register(&config.Option{
		Name:           "Block Peer to Peer Connections",
		Key:            CfgOptionBlockP2PKey,
		Description:    "These are connections that are established directly to an IP address on the Internet without resolving a domain name via DNS first.",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelExtreme,
		PossibleValues: status.SecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionBlockP2POrder,
			config.CategoryAnnotation:     "Scopes & Types",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockP2P = config.Concurrent.GetAsInt(CfgOptionBlockP2PKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionBlockP2PKey] = cfgOptionBlockP2P

	// Block Inbound Connections
	err = config.Register(&config.Option{
		Name:           "Block Inbound Connections",
		Key:            CfgOptionBlockInboundKey,
		Description:    "Connections initiated towards your device from the LAN or Internet. This will usually only be the case if you are running a network service or are using peer to peer software.",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelsHighAndExtreme,
		PossibleValues: status.SecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionBlockInboundOrder,
			config.CategoryAnnotation:     "Scopes & Types",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockInbound = config.Concurrent.GetAsInt(CfgOptionBlockInboundKey, int64(status.SecurityLevelsHighAndExtreme))
	cfgIntOptions[CfgOptionBlockInboundKey] = cfgOptionBlockInbound

	// Enforce SPN
	err = config.Register(&config.Option{
		Name:           "Enforce SPN",
		Key:            CfgOptionEnforceSPNKey,
		Description:    "This setting enforces connections to be routed over the SPN. If this is not possible for any reason, connections will be blocked.",
		OptType:        config.OptTypeInt,
		ReleaseLevel:   config.ReleaseLevelExperimental,
		DefaultValue:   status.SecurityLevelOff,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionEnforceSPNOrder,
			config.CategoryAnnotation: "Advanced",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionEnforceSPN = config.Concurrent.GetAsInt(CfgOptionEnforceSPNKey, int64(status.SecurityLevelOff))
	cfgIntOptions[CfgOptionEnforceSPNKey] = cfgOptionEnforceSPN

	// Filter Out-of-Scope DNS Records
	err = config.Register(&config.Option{
		Name:           "Filter Out-of-Scope DNS Records",
		Key:            CfgOptionRemoveOutOfScopeDNSKey,
		Description:    "Filter DNS answers that are outside of the scope of the server. A server on the public Internet may not respond with a private LAN address.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelBeta,
		DefaultValue:   status.SecurityLevelsAll,
		PossibleValues: status.SecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionRemoveOutOfScopeDNSOrder,
			config.CategoryAnnotation: "DNS",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionRemoveOutOfScopeDNS = config.Concurrent.GetAsInt(CfgOptionRemoveOutOfScopeDNSKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionRemoveOutOfScopeDNSKey] = cfgOptionRemoveOutOfScopeDNS

	// Filter DNS Records that would be blocked
	err = config.Register(&config.Option{
		Name:           "Filter DNS Records that would be blocked",
		Key:            CfgOptionRemoveBlockedDNSKey,
		Description:    "Pre-filter DNS answers that an application would not be allowed to connect to.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		ReleaseLevel:   config.ReleaseLevelBeta,
		DefaultValue:   status.SecurityLevelsAll,
		PossibleValues: status.SecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionRemoveBlockedDNSOrder,
			config.CategoryAnnotation: "DNS",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionRemoveBlockedDNS = config.Concurrent.GetAsInt(CfgOptionRemoveBlockedDNSKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionRemoveBlockedDNSKey] = cfgOptionRemoveBlockedDNS

	// Domain heuristics
	err = config.Register(&config.Option{
		Name:           "Enable Domain Heuristics",
		Key:            CfgOptionDomainHeuristicsKey,
		Description:    "Domain Heuristics checks for suspicious looking domain names and blocks them. Ths option currently targets domains generated by malware and DNS data tunnels.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		DefaultValue:   status.SecurityLevelsAll,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionDomainHeuristicsOrder,
			config.CategoryAnnotation: "DNS",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionDomainHeuristics = config.Concurrent.GetAsInt(CfgOptionDomainHeuristicsKey, int64(status.SecurityLevelsAll))

	// Bypass prevention
	err = config.Register(&config.Option{
		Name:           "Prevent Bypassing",
		Key:            CfgOptionPreventBypassingKey,
		Description:    "Prevent apps from bypassing the privacy filter: Firefox by disabling DNS-over-HTTPs",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelBeta,
		DefaultValue:   status.SecurityLevelsAll,
		PossibleValues: status.SecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionPreventBypassingOrder,
			config.CategoryAnnotation: "Advanced",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionPreventBypassing = config.Concurrent.GetAsInt((CfgOptionPreventBypassingKey), int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionPreventBypassingKey] = cfgOptionPreventBypassing

	return nil
}
