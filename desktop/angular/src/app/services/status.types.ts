import { getEnumKey, Record, ReleaseLevel, SecurityLevel } from '@safing/portmaster-api';

export interface CaptivePortal {
  URL: string;
  IP: string;
  Domain: string;
}

export enum ModuleStatus {
  Off = 0,
  Error = 1,
  Warning = 2,
  Operational = 3
}

/**
 * Returns a string represetnation of the module status.
 *
 * @param stat The module status to translate
 */
export function getModuleStatusString(stat: ModuleStatus): string {
  return getEnumKey(ModuleStatus, stat)
}

export enum OnlineStatus {
  Unknown = 0,
  Offline = 1,
  Limited = 2, // local network only,
  Portal = 3,
  SemiOnline = 4,
  Online = 5,
}

/**
 * Converts a online status value to a string.
 *
 * @param stat The online status value to convert
 */
export function getOnlineStatusString(stat: OnlineStatus): string {
  return getEnumKey(OnlineStatus, stat)
}

export interface Threat<T = any> {
  ID: string;
  Name: string;
  Description: string;
  AdditionalData: T;
  MitigationLevel: SecurityLevel;
  Started: number;
  Ended: number;
}

export interface CoreStatus extends Record {
  ActiveSecurityLevel: SecurityLevel;
  SelectedSecurityLevel: SecurityLevel;
  ThreatMitigationLevel: SecurityLevel;
  OnlineStatus: OnlineStatus;
  Threats: Threat[];
  CaptivePortal: CaptivePortal;
}

export enum FailureStatus {
  Operational = 0,
  Hint = 1,
  Warning = 2,
  Error = 3
}

/**
 * Returns a string representation of a failure status value.
 *
 * @param stat The failure status value.
 */
export function getFailureStatusString(stat: FailureStatus): string {
  return getEnumKey(FailureStatus, stat)
}

export interface Module {
  Enabled: boolean;
  FailureID: string;
  FailureMsg: string;
  FailureStatus: FailureStatus;
  Name: string;
  Status: ModuleStatus;
}

export interface Subsystem extends Record {
  ConfigKeySpace: string;
  Description: string;
  ExpertiseLevel: string;
  FailureStatus: FailureStatus;
  ID: string;
  Modules: Module[];
  Name: string;
  ReleaseLevel: ReleaseLevel;
  ToggleOptionKey: string;
}

export interface CoreVersion {
  BuildDate: string;
  BuildHost: string;
  BuildOptions: string;
  BuildSource: string;
  BuildUser: string;
  Commit: string;
  License: string;
  Name: string;
  Version: string;
}

export interface ResourceVersion {
  Available: boolean;
  BetaRelease: boolean;
  Blacklisted: boolean;
  StableRelease: boolean;
  VersionNumber: string;
}

export interface Resource {
  ActiveVersion: ResourceVersion | null;
  Identifier: string;
  SelectedVersion: ResourceVersion;
  Versions: ResourceVersion[];
}

export interface VersionStatus extends Record {
  Channel: string;
  Core: CoreVersion;
  Resources: {
    [key: string]: Resource
  }
}
