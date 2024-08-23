import { getEnumKey, Record, ReleaseLevel, SecurityLevel } from '@safing/portmaster-api';

export interface CaptivePortal {
  URL: string;
  IP: string;
  Domain: string;
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

export interface CoreStatus extends Record {
  OnlineStatus: OnlineStatus;
  CaptivePortal: CaptivePortal;
  // Modules: []ModuleState; // TODO: Do we need all modules?
  WorstState: {
    Module: string,
    ID: string,
    Name: string,
    Message: string,
    Type: ModuleStateType,
    // Time: time.Time, // TODO: How do we best use Go's time.Time?
    Data: any
  }
}

export enum ModuleStateType {
  Undefined = "",
  Hint = "hint",
  Warning = "warning",
  Error = "error"
}

/**
 * Returns a string representation of a failure status value.
 *
 * @param stateType The module state type value.
 */
export function getModuleStateString(stateType: ModuleStateType): string {
  return getEnumKey(ModuleStateType, stateType)
}

export interface Module {
  Enabled: boolean;
  Name: string;
}

export interface Subsystem extends Record {
  ConfigKeySpace: string;
  Description: string;
  ExpertiseLevel: string;
  ID: string;
  Modules: Module[];
  Name: string;
  ReleaseLevel: ReleaseLevel;
  ToggleOptionKey: string;
}

export interface CoreVersion {
  // Copied from base/info/version.go

  Name:          string;
  Version:       string;
  VersionNumber: string;
  License:       string;

  Source:    string;
  BuildTime: string;
  CGO:       boolean;

  Commit:     string;
  CommitTime: string;
  Dirty:      boolean;
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
