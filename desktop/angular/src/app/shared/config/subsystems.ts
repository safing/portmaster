import { ExpertiseLevelNumber } from "@safing/portmaster-api";
import { Subsystem } from "src/app/services/status.types";

export interface SubsystemWithExpertise extends Subsystem {
  minimumExpertise: ExpertiseLevelNumber;
  isDisabled: boolean;
  hasUserDefinedValues: boolean;
}

export var subsystems : SubsystemWithExpertise[] = [
  {
    minimumExpertise: ExpertiseLevelNumber.developer,
    isDisabled: false,
    hasUserDefinedValues: false,
    ID: "core",
    Name: "Core",
    Description: "Base Structure and System Integration",
    Modules: [
      {
        Name: "core",
        Enabled: true
      },
      {
        Name: "subsystems",
        Enabled: true
      },
      {
        Name: "runtime",
        Enabled: true
      },
      {
        Name: "status",
        Enabled: true
      },
      {
        Name: "ui",
        Enabled: true
      },
      {
        Name: "compat",
        Enabled: true
      },
      {
        Name: "broadcasts",
        Enabled: true
      },
      {
        Name: "sync",
        Enabled: true
      }
    ],
    ToggleOptionKey: "",
    ExpertiseLevel: "user",
    ReleaseLevel: 0,
    ConfigKeySpace: "config:core/",
    _meta: {
      Created: 0,
      Modified: 0,
      Expires: 0,
      Deleted: 0,
      Key: "runtime:subsystems/core"
    }
  },
  {
    minimumExpertise: ExpertiseLevelNumber.developer,
    isDisabled: false,
    hasUserDefinedValues: false,
    ID: "dns",
    Name: "Secure DNS",
    Description: "DNS resolver with scoping and DNS-over-TLS",
    Modules: [
      {
        Name: "nameserver",
        Enabled: true
      },
      {
        Name: "resolver",
        Enabled: true
      }
    ],
    ToggleOptionKey: "",
    ExpertiseLevel: "user",
    ReleaseLevel: 0,
    ConfigKeySpace: "config:dns/",
    _meta: {
      Created: 0,
      Modified: 0,
      Expires: 0,
      Deleted: 0,
      Key: "runtime:subsystems/dns"
    }
  },
  {
    minimumExpertise: ExpertiseLevelNumber.developer,
    isDisabled: false,
    hasUserDefinedValues: false,
    ID: "filter",
    Name: "Privacy Filter",
    Description: "DNS and Network Filter",
    Modules: [
      {
        Name: "filter",
        Enabled: true
      },
      {
        Name: "interception",
        Enabled: true
      },
      {
        Name: "base",
        Enabled: true
      },
      {
        Name: "database",
        Enabled: true
      },
      {
        Name: "config",
        Enabled: true
      },
      {
        Name: "rng",
        Enabled: true
      },
      {
        Name: "metrics",
        Enabled: true
      },
      {
        Name: "api",
        Enabled: true
      },
      {
        Name: "updates",
        Enabled: true
      },
      {
        Name: "network",
        Enabled: true
      },
      {
        Name: "netenv",
        Enabled: true
      },
      {
        Name: "processes",
        Enabled: true
      },
      {
        Name: "profiles",
        Enabled: true
      },
      {
        Name: "notifications",
        Enabled: true
      },
      {
        Name: "intel",
        Enabled: true
      },
      {
        Name: "geoip",
        Enabled: true
      },
      {
        Name: "filterlists",
        Enabled: true
      },
      {
        Name: "customlists",
        Enabled: true
      }
    ],
    ToggleOptionKey: "",
    ExpertiseLevel: "user",
    ReleaseLevel: 0,
    ConfigKeySpace: "config:filter/",
    _meta: {
      Created: 0,
      Modified: 0,
      Expires: 0,
      Deleted: 0,
      Key: "runtime:subsystems/filter"
    }
  },
  {
    minimumExpertise: ExpertiseLevelNumber.developer,
    isDisabled: false,
    hasUserDefinedValues: false,
    ID: "history",
    Name: "Network History",
    Description: "Keep Network History Data",
    Modules: [
      {
        Name: "netquery",
        Enabled: true
      }
    ],
    ToggleOptionKey: "",
    ExpertiseLevel: "user",
    ReleaseLevel: 0,
    ConfigKeySpace: "config:history/",
    _meta: {
      Created: 0,
      Modified: 0,
      Expires: 0,
      Deleted: 0,
      Key: "runtime:subsystems/history"
    }
  },
  {
    minimumExpertise: ExpertiseLevelNumber.developer,
    isDisabled: false,
    hasUserDefinedValues: false,
    ID: "spn",
    Name: "SPN",
    Description: "Safing Privacy Network",
    Modules: [
      {
        Name: "captain",
        Enabled: false
      },
      {
        Name: "terminal",
        Enabled: false
      },
      {
        Name: "cabin",
        Enabled: false
      },
      {
        Name: "ships",
        Enabled: false
      },
      {
        Name: "docks",
        Enabled: false
      },
      {
        Name: "access",
        Enabled: false
      },
      {
        Name: "crew",
        Enabled: false
      },
      {
        Name: "navigator",
        Enabled: false
      },
      {
        Name: "sluice",
        Enabled: false
      },
      {
        Name: "patrol",
        Enabled: false
      }
    ],
    ToggleOptionKey: "spn/enable",
    ExpertiseLevel: "user",
    ReleaseLevel: 0,
    ConfigKeySpace: "config:spn/",
    _meta: {
      Created: 0,
      Modified: 0,
      Expires: 0,
      Deleted: 0,
      Key: "runtime:subsystems/spn"
    }
  }
];
