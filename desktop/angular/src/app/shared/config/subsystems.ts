import { ExpertiseLevelNumber } from "@safing/portmaster-api";
import { ModuleStatus, Subsystem } from "src/app/services/status.types";

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
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "subsystems",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "runtime",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "status",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "ui",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "compat",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "broadcasts",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "sync",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      }
    ],
    FailureStatus: 0,
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
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "resolver",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      }
    ],
    FailureStatus: 0,
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
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "interception",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "base",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "database",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "config",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "rng",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "metrics",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "api",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "updates",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "network",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "netenv",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "processes",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "profiles",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "notifications",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "intel",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "geoip",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "filterlists",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "customlists",
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      }
    ],
    FailureStatus: 0,
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
        Enabled: true,
        Status: ModuleStatus.Operational,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      }
    ],
    FailureStatus: 0,
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
        Enabled: false,
        Status: 2,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "terminal",
        Enabled: false,
        Status: 2,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "cabin",
        Enabled: false,
        Status: 2,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "ships",
        Enabled: false,
        Status: 2,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "docks",
        Enabled: false,
        Status: 2,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "access",
        Enabled: false,
        Status: 2,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "crew",
        Enabled: false,
        Status: 2,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "navigator",
        Enabled: false,
        Status: 2,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "sluice",
        Enabled: false,
        Status: 2,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      },
      {
        Name: "patrol",
        Enabled: false,
        Status: 2,
        FailureStatus: 0,
        FailureID: "",
        FailureMsg: ""
      }
    ],
    FailureStatus: 0,
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
