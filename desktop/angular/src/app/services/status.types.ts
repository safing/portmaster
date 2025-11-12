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
  Modules: StateUpdate[]; // TODO: Do we need all modules?
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

export interface StateUpdate {
  Module: string;
  States: State[];
}

export interface State {  
  ID: string;             // Program-unique identifier  
  Name: string;           // State name (may serve as notification title)  
  Message?: string;       // Detailed message about the state  
  Type?: ModuleStateType; // State type  
  Time?: Date;            // Creation time  
  Data?: any;             // Additional data for processing
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

function getModuleStates(status: CoreStatus, moduleID: string): State[] {
  const module = status.Modules?.find(m => m.Module === moduleID);
  return module?.States || [];  
}

/**
 * Retrieves a specific state from a module within the CoreStatus.
 * @param status The CoreStatus object containing module states.
 * @param moduleID The identifier of the module to search within.
 * @param stateID The identifier of the state to retrieve.
 * @returns The State object if found; otherwise, null.
 * @example
 * ```typescript
 * const state = GetModuleState(status, 'Control', 'control:paused');
 * if (state) {
 *   console.log(`State found: ${state.Name}`);
 * } else {
 *   console.log('State not found');
 * }
 * ```
 */
export function GetModuleState(status: CoreStatus, moduleID: string, stateID: string): State | null {
  const states = getModuleStates(status, moduleID);
  for (const state of states) {
    if (state.ID === stateID) {
      return state;
    }
  }
  return null;
}

/**
 * Data structure for the 'control:paused' state from the 'Control' module.
 * 
 * This interface defines the expected structure of the Data field when Portmaster
 * or its components are temporarily paused by the user.
 * 
 * @example
 * ```typescript
 * const pausedState = GetModuleState(status, 'Control', 'control:paused');
 * if (pausedState?.Data) {
 *   const pauseData = pausedState.Data as ControlPauseStateData;
 *   console.log(`SPN paused: ${pauseData.SPN}`);
 * }
 * ```
 */
export interface ControlPauseStateData { 
    Interception: boolean;  // Whether Portmaster interception is paused
    SPN:          boolean;  // Whether SPN is paused    
    TillTime:     string;   // When the pause will end (JSON date as string, has to be converted to Date)
}
