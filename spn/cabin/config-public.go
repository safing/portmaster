package cabin

import (
	"fmt"
	"net"
	"os"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/profile/endpoints"
	"github.com/safing/portmaster/spn/hub"
)

// Configuration Keys.
var (
	// Name of the node.
	publicCfgOptionNameKey     = "spn/publicHub/name"
	publicCfgOptionName        config.StringOption
	publicCfgOptionNameDefault = ""
	publicCfgOptionNameOrder   = 512

	// Person or organisation, who is in control of the node (should be same for all nodes of this person or organisation).
	publicCfgOptionGroupKey     = "spn/publicHub/group"
	publicCfgOptionGroup        config.StringOption
	publicCfgOptionGroupDefault = ""
	publicCfgOptionGroupOrder   = 513

	// Contact possibility  (recommended, but optional).
	publicCfgOptionContactAddressKey     = "spn/publicHub/contactAddress"
	publicCfgOptionContactAddress        config.StringOption
	publicCfgOptionContactAddressDefault = ""
	publicCfgOptionContactAddressOrder   = 514

	// Type of service of the contact address, if not email.
	publicCfgOptionContactServiceKey     = "spn/publicHub/contactService"
	publicCfgOptionContactService        config.StringOption
	publicCfgOptionContactServiceDefault = ""
	publicCfgOptionContactServiceOrder   = 515

	// Hosters - supply chain (reseller, hosting provider, datacenter operator, ...).
	publicCfgOptionHostersKey     = "spn/publicHub/hosters"
	publicCfgOptionHosters        config.StringArrayOption
	publicCfgOptionHostersDefault = []string{}
	publicCfgOptionHostersOrder   = 516

	// Datacenter
	// Format: CC-COMPANY-INTERNALCODE
	// Eg: DE-Hetzner-FSN1-DC5
	//.
	publicCfgOptionDatacenterKey     = "spn/publicHub/datacenter"
	publicCfgOptionDatacenter        config.StringOption
	publicCfgOptionDatacenterDefault = ""
	publicCfgOptionDatacenterOrder   = 517

	// Network Location and Access.

	// IPv4 must be global and accessible.
	publicCfgOptionIPv4Key     = "spn/publicHub/ip4"
	publicCfgOptionIPv4        config.StringOption
	publicCfgOptionIPv4Default = ""
	publicCfgOptionIPv4Order   = 518

	// IPv6 must be global and accessible.
	publicCfgOptionIPv6Key     = "spn/publicHub/ip6"
	publicCfgOptionIPv6        config.StringOption
	publicCfgOptionIPv6Default = ""
	publicCfgOptionIPv6Order   = 519

	// Transports.
	publicCfgOptionTransportsKey     = "spn/publicHub/transports"
	publicCfgOptionTransports        config.StringArrayOption
	publicCfgOptionTransportsDefault = []string{
		"tcp:17",
	}
	publicCfgOptionTransportsOrder = 520

	// Entry Policy.
	publicCfgOptionEntryKey     = "spn/publicHub/entry"
	publicCfgOptionEntry        config.StringArrayOption
	publicCfgOptionEntryDefault = []string{}
	publicCfgOptionEntryOrder   = 521

	// Exit Policy.
	publicCfgOptionExitKey     = "spn/publicHub/exit"
	publicCfgOptionExit        config.StringArrayOption
	publicCfgOptionExitDefault = []string{"- * TCP/25"}
	publicCfgOptionExitOrder   = 522

	// Allow Unencrypted.
	publicCfgOptionAllowUnencryptedKey     = "spn/publicHub/allowUnencrypted"
	publicCfgOptionAllowUnencrypted        config.BoolOption
	publicCfgOptionAllowUnencryptedDefault = false
	publicCfgOptionAllowUnencryptedOrder   = 523
)

func prepPublicHubConfig() error {
	err := config.Register(&config.Option{
		Name:            "Name",
		Key:             publicCfgOptionNameKey,
		Description:     "Human readable name of the Hub.",
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		RequiresRestart: true,
		DefaultValue:    publicCfgOptionNameDefault,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: publicCfgOptionNameOrder,
		},
	})
	if err != nil {
		return err
	}
	publicCfgOptionName = config.GetAsString(publicCfgOptionNameKey, publicCfgOptionNameDefault)

	err = config.Register(&config.Option{
		Name:            "Group",
		Key:             publicCfgOptionGroupKey,
		Description:     "Name of the hub group this Hub belongs to.",
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		RequiresRestart: true,
		DefaultValue:    publicCfgOptionGroupDefault,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: publicCfgOptionGroupOrder,
		},
	})
	if err != nil {
		return err
	}
	publicCfgOptionGroup = config.GetAsString(publicCfgOptionGroupKey, publicCfgOptionGroupDefault)

	err = config.Register(&config.Option{
		Name:            "Contact Address",
		Key:             publicCfgOptionContactAddressKey,
		Description:     "Contact address where the Hub operator can be reached.",
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		RequiresRestart: true,
		DefaultValue:    publicCfgOptionContactAddressDefault,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: publicCfgOptionContactAddressOrder,
		},
	})
	if err != nil {
		return err
	}
	publicCfgOptionContactAddress = config.GetAsString(publicCfgOptionContactAddressKey, publicCfgOptionContactAddressDefault)

	err = config.Register(&config.Option{
		Name:            "Contact Service",
		Key:             publicCfgOptionContactServiceKey,
		Description:     "Name of the service the contact address corresponds to, if not email.",
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		RequiresRestart: true,
		DefaultValue:    publicCfgOptionContactServiceDefault,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: publicCfgOptionContactServiceOrder,
		},
	})
	if err != nil {
		return err
	}
	publicCfgOptionContactService = config.GetAsString(publicCfgOptionContactServiceKey, publicCfgOptionContactServiceDefault)

	err = config.Register(&config.Option{
		Name:            "Hosters",
		Key:             publicCfgOptionHostersKey,
		Description:     "List of all involved entities and organisations that are involved in hosting this Hub.",
		OptType:         config.OptTypeStringArray,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		RequiresRestart: true,
		DefaultValue:    publicCfgOptionHostersDefault,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: publicCfgOptionHostersOrder,
		},
	})
	if err != nil {
		return err
	}
	publicCfgOptionHosters = config.GetAsStringArray(publicCfgOptionHostersKey, publicCfgOptionHostersDefault)

	err = config.Register(&config.Option{
		Name:            "Datacenter",
		Key:             publicCfgOptionDatacenterKey,
		Description:     "Identifier of the datacenter this Hub is hosted in.",
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		RequiresRestart: true,
		DefaultValue:    publicCfgOptionDatacenterDefault,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: publicCfgOptionDatacenterOrder,
		},
	})
	if err != nil {
		return err
	}
	publicCfgOptionDatacenter = config.GetAsString(publicCfgOptionDatacenterKey, publicCfgOptionDatacenterDefault)

	err = config.Register(&config.Option{
		Name:            "IPv4",
		Key:             publicCfgOptionIPv4Key,
		Description:     "IPv4 address of this Hub. Must be globally reachable.",
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		RequiresRestart: true,
		DefaultValue:    publicCfgOptionIPv4Default,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: publicCfgOptionIPv4Order,
		},
	})
	if err != nil {
		return err
	}
	publicCfgOptionIPv4 = config.GetAsString(publicCfgOptionIPv4Key, publicCfgOptionIPv4Default)

	err = config.Register(&config.Option{
		Name:            "IPv6",
		Key:             publicCfgOptionIPv6Key,
		Description:     "IPv6 address of this Hub. Must be globally reachable.",
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		RequiresRestart: true,
		DefaultValue:    publicCfgOptionIPv6Default,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: publicCfgOptionIPv6Order,
		},
	})
	if err != nil {
		return err
	}
	publicCfgOptionIPv6 = config.GetAsString(publicCfgOptionIPv6Key, publicCfgOptionIPv6Default)

	err = config.Register(&config.Option{
		Name:            "Transports",
		Key:             publicCfgOptionTransportsKey,
		Description:     "List of transports this Hub supports.",
		OptType:         config.OptTypeStringArray,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		RequiresRestart: true,
		DefaultValue:    publicCfgOptionTransportsDefault,
		ValidationFunc: func(value any) error {
			if transports, ok := value.([]string); ok {
				for i, transport := range transports {
					if _, err := hub.ParseTransport(transport); err != nil {
						return fmt.Errorf("failed to parse transport #%d: %w", i, err)
					}
				}
			} else {
				return fmt.Errorf("not a []string, but %T", value)
			}
			return nil
		},
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: publicCfgOptionTransportsOrder,
		},
	})
	if err != nil {
		return err
	}
	publicCfgOptionTransports = config.GetAsStringArray(publicCfgOptionTransportsKey, publicCfgOptionTransportsDefault)

	err = config.Register(&config.Option{
		Name:            "Entry",
		Key:             publicCfgOptionEntryKey,
		Description:     "Define an entry policy. The format is the same for the endpoint lists. Default is permit.",
		OptType:         config.OptTypeStringArray,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		RequiresRestart: true,
		DefaultValue:    publicCfgOptionEntryDefault,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: publicCfgOptionEntryOrder,
			config.DisplayHintAnnotation:  endpoints.DisplayHintEndpointList,
		},
	})
	if err != nil {
		return err
	}
	publicCfgOptionEntry = config.GetAsStringArray(publicCfgOptionEntryKey, publicCfgOptionEntryDefault)

	err = config.Register(&config.Option{
		Name:            "Exit",
		Key:             publicCfgOptionExitKey,
		Description:     "Define an exit policy. The format is the same for the endpoint lists. Default is permit.",
		OptType:         config.OptTypeStringArray,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		RequiresRestart: true,
		DefaultValue:    publicCfgOptionExitDefault,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: publicCfgOptionExitOrder,
			config.DisplayHintAnnotation:  endpoints.DisplayHintEndpointList,
		},
	})
	if err != nil {
		return err
	}
	publicCfgOptionExit = config.GetAsStringArray(publicCfgOptionExitKey, publicCfgOptionExitDefault)

	err = config.Register(&config.Option{
		Name:            "Allow Unencrypted Connections",
		Key:             publicCfgOptionAllowUnencryptedKey,
		Description:     "Advertise that this Hub is available for handling unencrypted connections, as detected by clients.",
		OptType:         config.OptTypeBool,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		RequiresRestart: true,
		DefaultValue:    publicCfgOptionAllowUnencryptedDefault,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: publicCfgOptionAllowUnencryptedOrder,
		},
	})
	if err != nil {
		return err
	}
	publicCfgOptionAllowUnencrypted = config.GetAsBool(publicCfgOptionAllowUnencryptedKey, publicCfgOptionAllowUnencryptedDefault)

	// update defaults from system
	setDynamicPublicDefaults()

	return nil
}

func getPublicHubInfo() *hub.Announcement {
	// get configuration
	info := &hub.Announcement{
		Name:           publicCfgOptionName(),
		Group:          publicCfgOptionGroup(),
		ContactAddress: publicCfgOptionContactAddress(),
		ContactService: publicCfgOptionContactService(),
		Hosters:        publicCfgOptionHosters(),
		Datacenter:     publicCfgOptionDatacenter(),
		Transports:     publicCfgOptionTransports(),
		Entry:          publicCfgOptionEntry(),
		Exit:           publicCfgOptionExit(),
		Flags:          []string{},
	}

	if publicCfgOptionAllowUnencrypted() {
		info.Flags = append(info.Flags, hub.FlagAllowUnencrypted)
	}

	ip4 := publicCfgOptionIPv4()
	if ip4 != "" {
		ip := net.ParseIP(ip4)
		if ip == nil {
			log.Warningf("spn/cabin: invalid %s config: %s", publicCfgOptionIPv4Key, ip4)
		} else {
			info.IPv4 = ip
		}
	}

	ip6 := publicCfgOptionIPv6()
	if ip6 != "" {
		ip := net.ParseIP(ip6)
		if ip == nil {
			log.Warningf("spn/cabin: invalid %s config: %s", publicCfgOptionIPv6Key, ip6)
		} else {
			info.IPv6 = ip
		}
	}

	return info
}

func setDynamicPublicDefaults() {
	// name
	hostname, err := os.Hostname()
	if err == nil {
		err := config.SetDefaultConfigOption(publicCfgOptionNameKey, hostname)
		if err != nil {
			log.Warningf("spn/cabin: failed to set %s default to %s", publicCfgOptionNameKey, hostname)
		}
	}

	// IPs
	v4IPs, v6IPs, err := netenv.GetAssignedGlobalAddresses()
	if err != nil {
		log.Warningf("spn/cabin: failed to get assigned addresses: %s", err)
		return
	}
	if len(v4IPs) == 1 {
		err = config.SetDefaultConfigOption(publicCfgOptionIPv4Key, v4IPs[0].String())
		if err != nil {
			log.Warningf("spn/cabin: failed to set %s default to %s", publicCfgOptionIPv4Key, v4IPs[0].String())
		}
	}
	if len(v6IPs) == 1 {
		err = config.SetDefaultConfigOption(publicCfgOptionIPv6Key, v6IPs[0].String())
		if err != nil {
			log.Warningf("spn/cabin: failed to set %s default to %s", publicCfgOptionIPv6Key, v6IPs[0].String())
		}
	}
}
