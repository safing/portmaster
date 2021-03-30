package profile

import (
	"strings"

	"github.com/safing/portbase/config"
	"github.com/safing/portmaster/profile/endpoints"
	"github.com/safing/portmaster/status"
)

// Configuration Keys.
var (
	cfgStringOptions      = make(map[string]config.StringOption)
	cfgStringArrayOptions = make(map[string]config.StringArrayOption)
	cfgIntOptions         = make(map[string]config.IntOption)
	cfgBoolOptions        = make(map[string]config.BoolOption)

	// General

	// Enable Filter Order = 0

	CfgOptionDefaultActionKey   = "filter/defaultAction"
	cfgOptionDefaultAction      config.StringOption
	cfgOptionDefaultActionOrder = 1

	// Prompt Desktop Notifications Order = 2
	// Prompt Timeout Order = 3

	// Network Scopes

	CfgOptionBlockScopeInternetKey   = "filter/blockInternet"
	cfgOptionBlockScopeInternet      config.IntOption // security level option
	cfgOptionBlockScopeInternetOrder = 16

	CfgOptionBlockScopeLANKey   = "filter/blockLAN"
	cfgOptionBlockScopeLAN      config.IntOption // security level option
	cfgOptionBlockScopeLANOrder = 17

	CfgOptionBlockScopeLocalKey   = "filter/blockLocal"
	cfgOptionBlockScopeLocal      config.IntOption // security level option
	cfgOptionBlockScopeLocalOrder = 18

	// Connection Types

	CfgOptionBlockP2PKey   = "filter/blockP2P"
	cfgOptionBlockP2P      config.IntOption // security level option
	cfgOptionBlockP2POrder = 19

	CfgOptionBlockInboundKey   = "filter/blockInbound"
	cfgOptionBlockInbound      config.IntOption // security level option
	cfgOptionBlockInboundOrder = 20

	// Rules

	CfgOptionEndpointsKey   = "filter/endpoints"
	cfgOptionEndpoints      config.StringArrayOption
	cfgOptionEndpointsOrder = 32

	CfgOptionServiceEndpointsKey   = "filter/serviceEndpoints"
	cfgOptionServiceEndpoints      config.StringArrayOption
	cfgOptionServiceEndpointsOrder = 33

	CfgOptionFilterListsKey   = "filter/lists"
	cfgOptionFilterLists      config.StringArrayOption
	cfgOptionFilterListsOrder = 34

	CfgOptionFilterSubDomainsKey   = "filter/includeSubdomains"
	cfgOptionFilterSubDomains      config.IntOption // security level option
	cfgOptionFilterSubDomainsOrder = 35

	// DNS Filtering

	CfgOptionFilterCNAMEKey   = "filter/includeCNAMEs"
	cfgOptionFilterCNAME      config.IntOption // security level option
	cfgOptionFilterCNAMEOrder = 48

	CfgOptionRemoveOutOfScopeDNSKey   = "filter/removeOutOfScopeDNS"
	cfgOptionRemoveOutOfScopeDNS      config.IntOption // security level option
	cfgOptionRemoveOutOfScopeDNSOrder = 49

	CfgOptionRemoveBlockedDNSKey   = "filter/removeBlockedDNS"
	cfgOptionRemoveBlockedDNS      config.IntOption // security level option
	cfgOptionRemoveBlockedDNSOrder = 50

	CfgOptionDomainHeuristicsKey   = "filter/domainHeuristics"
	cfgOptionDomainHeuristics      config.IntOption // security level option
	cfgOptionDomainHeuristicsOrder = 51

	// Advanced

	CfgOptionPreventBypassingKey   = "filter/preventBypassing"
	cfgOptionPreventBypassing      config.IntOption // security level option
	cfgOptionPreventBypassingOrder = 64

	CfgOptionDisableAutoPermitKey   = "filter/disableAutoPermit"
	cfgOptionDisableAutoPermit      config.IntOption // security level option
	cfgOptionDisableAutoPermitOrder = 65

	// Permanent Verdicts Order = 96

	CfgOptionUseSPNKey   = "spn/useSPN"
	cfgOptionUseSPN      config.BoolOption
	cfgOptionUseSPNOrder = 129
)

func registerConfiguration() error {
	// Default Filter Action
	// permit - blocklist mode: everything is allowed unless blocked
	// ask - ask mode: if not verdict is found, the user is consulted
	// block - allowlist mode: everything is blocked unless explicitly allowed
	err := config.Register(&config.Option{
		Name:         "Default Action",
		Key:          CfgOptionDefaultActionKey,
		Description:  `The default action when nothing else allows or blocks an outgoing connection. Incoming connections are always blocked by default.`,
		OptType:      config.OptTypeString,
		DefaultValue: "permit",
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  config.DisplayHintOneOf,
			config.DisplayOrderAnnotation: cfgOptionDefaultActionOrder,
			config.CategoryAnnotation:     "General",
		},
		PossibleValues: []config.PossibleValue{
			{
				Name:        "Allow",
				Value:       "permit",
				Description: "Allow all connections",
			},
			{
				Name:        "Block",
				Value:       "block",
				Description: "Block all connections",
			},
			{
				Name:        "Prompt",
				Value:       "ask",
				Description: "Prompt for decisions",
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
		// TODO: Check how to best handle negation here.
		Name:         "Disable Auto Allow",
		Key:          CfgOptionDisableAutoPermitKey,
		Description:  `Auto Allow searches for a relation between an app and the destination of a connection - if there is a correlation, the connection will be allowed.`,
		OptType:      config.OptTypeInt,
		ReleaseLevel: config.ReleaseLevelBeta,
		DefaultValue: status.SecurityLevelsAll,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionDisableAutoPermitOrder,
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.CategoryAnnotation:     "Advanced",
		},
		PossibleValues: status.SecurityLevelValues,
	})
	if err != nil {
		return err
	}
	cfgOptionDisableAutoPermit = config.Concurrent.GetAsInt(CfgOptionDisableAutoPermitKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionDisableAutoPermitKey] = cfgOptionDisableAutoPermit

	rulesHelp := strings.ReplaceAll(`Rules are checked from top to bottom, stopping after the first match. They can match:

- By address: "192.168.0.1"
- By network: "192.168.0.1/24"
- By domain:
	- Matching a distinct domain: "example.com"
	- Matching a domain with subdomains: ".example.com"
	- Matching with a wildcard prefix: "*xample.com"
	- Matching with a wildcard suffix: "example.*"
	- Matching domains containing text: "*example*"
- By country (based on IP): "US"
- By filter list - use the filterlist ID prefixed with "L:": "L:MAL"
- Match anything: "*"

Additionally, you may supply a protocol and port just behind that using numbers ("6/80") or names ("TCP/HTTP").  
In this case the rule is only matched if the protocol and port also match.  
Example: "192.168.0.1 TCP/HTTP"
`, `"`, "`")

	// Endpoint Filter List
	err = config.Register(&config.Option{
		Name:         "Outgoing Rules",
		Key:          CfgOptionEndpointsKey,
		Description:  "Rules that apply to outgoing network connections. Cannot overrule Network Scopes and Connection Types (see above).",
		Help:         rulesHelp,
		OptType:      config.OptTypeStringArray,
		DefaultValue: []string{},
		Annotations: config.Annotations{
			config.StackableAnnotation:    true,
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
		Name:           "Incoming Rules",
		Key:            CfgOptionServiceEndpointsKey,
		Description:    "Rules that apply to incoming network connections. Cannot overrule Network Scopes and Connection Types (see above). Also note that the default action for incoming connections is to always block.",
		Help:           rulesHelp,
		OptType:        config.OptTypeStringArray,
		DefaultValue:   []string{"+ Localhost"},
		ExpertiseLevel: config.ExpertiseLevelExpert,
		Annotations: config.Annotations{
			config.StackableAnnotation:    true,
			config.DisplayHintAnnotation:  endpoints.DisplayHintEndpointList,
			config.DisplayOrderAnnotation: cfgOptionServiceEndpointsOrder,
			config.CategoryAnnotation:     "Rules",
			config.QuickSettingsAnnotation: []config.QuickSetting{
				{
					Name:   "SSH",
					Action: config.QuickMergeTop,
					Value:  []string{"+ * tcp/22"},
				},
				{
					Name:   "HTTP/s",
					Action: config.QuickMergeTop,
					Value:  []string{"+ * tcp/80", "+ * tcp/443"},
				},
				{
					Name:   "RDP",
					Action: config.QuickMergeTop,
					Value:  []string{"+ * */3389"},
				},
			},
		},
		ValidationRegex: `^(\+|\-) [A-z0-9\.:\-*/]+( [A-z0-9/]+)?$`,
	})
	if err != nil {
		return err
	}
	cfgOptionServiceEndpoints = config.Concurrent.GetAsStringArray(CfgOptionServiceEndpointsKey, []string{})
	cfgStringArrayOptions[CfgOptionServiceEndpointsKey] = cfgOptionServiceEndpoints

	filterListsHelp := strings.ReplaceAll(`Filter lists contain domains and IP addresses that are known to be used adversarial. The data is collected from many public sources and put into the following categories. In order to active a category, add it's "ID" to the list.

**Ads & Trackers** - ID: "TRAC"  
Services that track and profile people online, including as ads, analytics and telemetry.

**Malware** - ID: "MAL"  
Services that are (ab)used for attacking devices through technical means.

**Deception** - ID: "DECEP"  
Services that trick humans into thinking the service is genuine, while it is not, including phishing, fake news and fraud.

**Bad Stuff (Mixed)** - ID: "BAD"  
Miscellaneous services that are believed to be harmful to security or privacy, but their exact use is unknown, not categorized, or lists have mixed categories.

**NSFW** - ID: "NSFW"  
Services that are generally not accepted in work environments, including pornography, violence and gambling.

The lists are automatically updated every hour using incremental updates.  
[See here](https://github.com/safing/intel-data) for more detail about these lists, their sources and how to help to improve them.
`, `"`, "`")

	// Filter list IDs
	err = config.Register(&config.Option{
		Name:         "Filter Lists",
		Key:          CfgOptionFilterListsKey,
		Description:  "Block connections that match enabled filter lists.",
		Help:         filterListsHelp,
		OptType:      config.OptTypeStringArray,
		DefaultValue: []string{"TRAC", "MAL", "BAD"},
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  "filter list",
			config.DisplayOrderAnnotation: cfgOptionFilterListsOrder,
			config.CategoryAnnotation:     "Filter Lists",
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
		Name:           "Block Domain Aliases",
		Key:            CfgOptionFilterCNAMEKey,
		Description:    "Block a domain if a resolved CNAME (alias) is blocked by a rule or filter list.",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelsAll,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionFilterCNAMEOrder,
			config.CategoryAnnotation:     "DNS Filtering",
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
		Name:           "Block Subdomains of Filter List Entries",
		Key:            CfgOptionFilterSubDomainsKey,
		Description:    "Additionally block all subdomains of entries in selected filter lists.",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelsAll,
		PossibleValues: status.SecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionFilterSubDomainsOrder,
			config.CategoryAnnotation:     "Filter Lists",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionFilterSubDomains = config.Concurrent.GetAsInt(CfgOptionFilterSubDomainsKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionFilterSubDomainsKey] = cfgOptionFilterSubDomains

	// Block Scope Local
	err = config.Register(&config.Option{
		Name:           "Block Device-Local Connections",
		Key:            CfgOptionBlockScopeLocalKey,
		Description:    "Block all internal connections on your own device, ie. localhost. Is stronger than Rules (see below).",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		DefaultValue:   status.SecurityLevelOff,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionBlockScopeLocalOrder,
			config.CategoryAnnotation:     "Network Scope",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeLocal = config.Concurrent.GetAsInt(CfgOptionBlockScopeLocalKey, int64(status.SecurityLevelOff))
	cfgIntOptions[CfgOptionBlockScopeLocalKey] = cfgOptionBlockScopeLocal

	// Block Scope LAN
	err = config.Register(&config.Option{
		Name:           "Block LAN",
		Key:            CfgOptionBlockScopeLANKey,
		Description:    "Block all connections from and to the Local Area Network. Is stronger than Rules (see below).",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelsHighAndExtreme,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionBlockScopeLANOrder,
			config.CategoryAnnotation:     "Network Scope",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeLAN = config.Concurrent.GetAsInt(CfgOptionBlockScopeLANKey, int64(status.SecurityLevelsHighAndExtreme))
	cfgIntOptions[CfgOptionBlockScopeLANKey] = cfgOptionBlockScopeLAN

	// Block Scope Internet
	err = config.Register(&config.Option{
		Name:           "Block Internet Access",
		Key:            CfgOptionBlockScopeInternetKey,
		Description:    "Block connections from and to the Internet. Is stronger than Rules (see below).",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelOff,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionBlockScopeInternetOrder,
			config.CategoryAnnotation:     "Network Scope",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockScopeInternet = config.Concurrent.GetAsInt(CfgOptionBlockScopeInternetKey, int64(status.SecurityLevelOff))
	cfgIntOptions[CfgOptionBlockScopeInternetKey] = cfgOptionBlockScopeInternet

	// Block Peer to Peer Connections
	err = config.Register(&config.Option{
		Name:           "Block P2P/Direct Connections",
		Key:            CfgOptionBlockP2PKey,
		Description:    "These are connections that are established directly to an IP address or peer on the Internet without resolving a domain name via DNS first. Is stronger than Rules (see below).",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelExtreme,
		PossibleValues: status.SecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionBlockP2POrder,
			config.CategoryAnnotation:     "Connection Types",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockP2P = config.Concurrent.GetAsInt(CfgOptionBlockP2PKey, int64(status.SecurityLevelExtreme))
	cfgIntOptions[CfgOptionBlockP2PKey] = cfgOptionBlockP2P

	// Block Inbound Connections
	err = config.Register(&config.Option{
		Name:           "Block Incoming Connections",
		Key:            CfgOptionBlockInboundKey,
		Description:    "Connections initiated towards your device from the LAN or Internet. This will usually only be the case if you are running a network service or are using peer to peer software. Is stronger than Rules (see below).",
		OptType:        config.OptTypeInt,
		DefaultValue:   status.SecurityLevelsHighAndExtreme,
		PossibleValues: status.SecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionBlockInboundOrder,
			config.CategoryAnnotation:     "Connection Types",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionBlockInbound = config.Concurrent.GetAsInt(CfgOptionBlockInboundKey, int64(status.SecurityLevelsHighAndExtreme))
	cfgIntOptions[CfgOptionBlockInboundKey] = cfgOptionBlockInbound

	// Filter Out-of-Scope DNS Records
	err = config.Register(&config.Option{
		Name:           "Enforce Global/Private Split-View",
		Key:            CfgOptionRemoveOutOfScopeDNSKey,
		Description:    "Reject private IP addresses (RFC1918 et al.) from public DNS responses. If the system resolver is in use, the resulting connection will be blocked instead of the DNS request.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		DefaultValue:   status.SecurityLevelsAll,
		PossibleValues: status.SecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionRemoveOutOfScopeDNSOrder,
			config.CategoryAnnotation:     "DNS Filtering",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionRemoveOutOfScopeDNS = config.Concurrent.GetAsInt(CfgOptionRemoveOutOfScopeDNSKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionRemoveOutOfScopeDNSKey] = cfgOptionRemoveOutOfScopeDNS

	// Filter DNS Records that would be blocked
	err = config.Register(&config.Option{
		Name:           "Reject Blocked IPs",
		Key:            CfgOptionRemoveBlockedDNSKey,
		Description:    "Reject blocked IP addresses directly from the DNS response instead of handing them over to the app and blocking a resulting connection. This settings does not affect privacy and only takes effect when the system resolver is not in use.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelDeveloper,
		DefaultValue:   status.SecurityLevelsAll,
		PossibleValues: status.SecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionRemoveBlockedDNSOrder,
			config.CategoryAnnotation:     "DNS Filtering",
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
		Description:    "Checks for suspicious domain names and blocks them. This option currently targets domain names generated by malware and DNS data exfiltration channels.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelExpert,
		DefaultValue:   status.SecurityLevelsAll,
		PossibleValues: status.AllSecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionDomainHeuristicsOrder,
			config.CategoryAnnotation:     "DNS Filtering",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionDomainHeuristics = config.Concurrent.GetAsInt(CfgOptionDomainHeuristicsKey, int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionDomainHeuristicsKey] = cfgOptionDomainHeuristics

	// Bypass prevention
	err = config.Register(&config.Option{
		Name: "Block Bypassing",
		Key:  CfgOptionPreventBypassingKey,
		Description: `Prevent apps from bypassing the privacy filter.  
Current Features:  
- Disable Firefox' internal DNS-over-HTTPs resolver
- Block direct access to public DNS resolvers

Please note that if you are using the system resolver, bypass attempts might be additionally blocked there too.`,
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelBeta,
		DefaultValue:   status.SecurityLevelsAll,
		PossibleValues: status.SecurityLevelValues,
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  status.DisplayHintSecurityLevel,
			config.DisplayOrderAnnotation: cfgOptionPreventBypassingOrder,
			config.CategoryAnnotation:     "Advanced",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionPreventBypassing = config.Concurrent.GetAsInt((CfgOptionPreventBypassingKey), int64(status.SecurityLevelsAll))
	cfgIntOptions[CfgOptionPreventBypassingKey] = cfgOptionPreventBypassing

	// Use SPN
	err = config.Register(&config.Option{
		Name:         "Use SPN",
		Key:          CfgOptionUseSPNKey,
		Description:  "Route connections through the Safing Privacy Network. If it is disabled or unavailable for any reason, connections will be blocked.",
		OptType:      config.OptTypeBool,
		DefaultValue: true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionUseSPNOrder,
			config.CategoryAnnotation:     "General",
		},
	})
	if err != nil {
		return err
	}
	cfgOptionUseSPN = config.Concurrent.GetAsBool(CfgOptionUseSPNKey, true)
	cfgBoolOptions[CfgOptionUseSPNKey] = cfgOptionUseSPN

	return nil
}
