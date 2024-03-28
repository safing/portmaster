import { FeatureID } from './features';
import { Record } from './portapi.types';
import { deepClone } from './utils';

/**
 * ExpertiseLevel defines all available expertise levels.
 */
export enum ExpertiseLevel {
  User = 'user',
  Expert = 'expert',
  Developer = 'developer',
}

export enum ExpertiseLevelNumber {
  user = 0,
  expert = 1,
  developer = 2
}

export function getExpertiseLevelNumber(lvl: ExpertiseLevel): ExpertiseLevelNumber {
  switch (lvl) {
    case ExpertiseLevel.User:
      return ExpertiseLevelNumber.user;
    case ExpertiseLevel.Expert:
      return ExpertiseLevelNumber.expert;
    case ExpertiseLevel.Developer:
      return ExpertiseLevelNumber.developer
  }
}

/**
 * OptionType defines the type of an option as stored in
 * the backend. Note that ExternalOptionHint may be used
 * to request a different visual representation and edit
 * menu on a per-option basis.
 */
export enum OptionType {
  String = 1,
  StringArray = 2,
  Int = 3,
  Bool = 4,
}

/**
 * Converts an option type to it's string representation.
 *
 * @param opt The option type to convert
 */
export function optionTypeName(opt: OptionType): string {
  switch (opt) {
    case OptionType.String:
      return 'string';
    case OptionType.StringArray:
      return '[]string';
    case OptionType.Int:
      return 'int'
    case OptionType.Bool:
      return 'bool'
  }
}

/** The actual type an option value can be */
export type OptionValueType = string | string[] | number | boolean;

/** Type-guard for string option types */
export function isStringType(opt: OptionType, vt: OptionValueType): vt is string {
  return opt === OptionType.String;
}

/** Type-guard for string-array option types */
export function isStringArrayType(opt: OptionType, vt: OptionValueType): vt is string[] {
  return opt === OptionType.StringArray;
}

/** Type-guard for number option types */
export function isNumberType(opt: OptionType, vt: OptionValueType): vt is number {
  return opt === OptionType.Int;
}

/** Type-guard for boolean option types */
export function isBooleanType(opt: OptionType, vt: OptionValueType): vt is boolean {
  return opt === OptionType.Bool;
}

/**
 * ReleaseLevel defines the available release and maturity
 * levels.
 */
export enum ReleaseLevel {
  Stable = 0,
  Beta = 1,
  Experimental = 2,
}

export function releaseLevelFromName(name: 'stable' | 'beta' | 'experimental'): ReleaseLevel {
  switch (name) {
    case 'stable':
      return ReleaseLevel.Stable;
    case 'beta':
      return ReleaseLevel.Beta;
    case 'experimental':
      return ReleaseLevel.Experimental;
  }
}

/**
 * releaseLevelName returns a string representation of the
 * release level.
 *
 * @args level The release level to convert.
 */
export function releaseLevelName(level: ReleaseLevel): string {
  switch (level) {
    case ReleaseLevel.Stable:
      return 'stable'
    case ReleaseLevel.Beta:
      return 'beta'
    case ReleaseLevel.Experimental:
      return 'experimental'
  }
}

/**
 * ExternalOptionHint tells the UI to use a different visual
 * representation and edit menu that the options value would
 * imply.
 */
export enum ExternalOptionHint {
  SecurityLevel = 'security level',
  EndpointList = 'endpoint list',
  FilterList = 'filter list',
  OneOf = 'one-of',
  OrderedList = 'ordered'
}

/** A list of well-known option annotation keys. */
export enum WellKnown {
  DisplayHint = "safing/portbase:ui:display-hint",
  Order = "safing/portbase:ui:order",
  Unit = "safing/portbase:ui:unit",
  Category = "safing/portbase:ui:category",
  Subsystem = "safing/portbase:module:subsystem",
  Stackable = "safing/portbase:options:stackable",
  QuickSetting = "safing/portbase:ui:quick-setting",
  Requires = "safing/portbase:config:requires",
  RestartPending = "safing/portbase:options:restart-pending",
  EndpointListVerdictNames = "safing/portmaster:ui:endpoint-list:verdict-names",
  RequiresFeatureID = "safing/portmaster:ui:config:requires-feature",
  RequiresUIReload = "safing/portmaster:ui:requires-reload",
}

/**
 * Annotations describes the annoations object of a configuration
 * setting. Well-known annotations are stricktly typed.
 */
export interface Annotations<T extends OptionValueType> {
  // Well known option annoations and their
  // types.
  [WellKnown.DisplayHint]?: ExternalOptionHint;
  [WellKnown.Order]?: number;
  [WellKnown.Unit]?: string;
  [WellKnown.Category]?: string;
  [WellKnown.Subsystem]?: string;
  [WellKnown.Stackable]?: true;
  [WellKnown.QuickSetting]?: QuickSetting<T> | QuickSetting<T>[] | CountrySelectionQuickSetting<T> | CountrySelectionQuickSetting<T>[];
  [WellKnown.Requires]?: ValueRequirement | ValueRequirement[];
  [WellKnown.RequiresFeatureID]?: FeatureID | FeatureID[];
  [WellKnown.RequiresUIReload]?: unknown,
  // Any thing else...
  [key: string]: any;
}

export interface PossilbeValue<T = any> {
  /** Name is the name of the value and should be displayed */
  Name: string;
  /** Description may hold an additional description of the value */
  Description: string;
  /** Value is the actual value expected by the portmaster */
  Value: T;
}

export interface QuickSetting<T extends OptionValueType> {
  // Name is the name of the quick setting.
  Name: string;
  // Value is the value that the quick-setting configures. It must match
  // the expected value type of the annotated option.
  Value: T;
  // Action defines the action of the quick setting.
  Action: 'replace' | 'merge-top' | 'merge-bottom';
}

export interface CountrySelectionQuickSetting<T extends OptionValueType> extends QuickSetting<T> {
  // Filename of the flag to be used.
  // In most cases this will be the 2-letter country code, but there are also special flags.
  FlagID: string;
}

export interface ValueRequirement {
  // Key is the configuration key of the required setting.
  Key: string;
  // Value is the required value of the linked setting.
  Value: any;
}

/**
 * BaseSetting describes the general shape of a portbase config setting.
 */
export interface BaseSetting<T extends OptionValueType, O extends OptionType> extends Record {
  // Value is the value of a setting.
  Value?: T;
  // DefaultValue is the default value of a setting.
  DefaultValue: T;
  // Description is a short description.
  Description?: string;
  // ExpertiseLevel defines the required expertise level for
  // this setting to show up.
  ExpertiseLevel: ExpertiseLevelNumber;
  // Help may contain a longer help text for this option.
  Help?: string;
  // Key is the database key.
  Key: string;
  // Name is the name of the option.
  Name: string;
  // OptType is the option's basic type.
  OptType: O;
  // Annotations holds option specific annotations.
  Annotations: Annotations<T>;
  // ReleaseLevel defines the release level of the feature
  // or settings changed by this option.
  ReleaseLevel: ReleaseLevel;
  // RequiresRestart may be set to true if the service requires
  // a restart after this option has been changed.
  RequiresRestart?: boolean;
  // ValidateRegex defines the regex used to validate this option.
  // The regex is used in Golang but is expected to be valid in
  // JavaScript as well.
  ValidationRegex?: string;
  PossibleValues?: PossilbeValue[];

  // GlobalDefault holds the global default value and is used in the app settings
  // This property is NOT defined inside the portmaster!
  GlobalDefault?: T;
}

export type IntSetting = BaseSetting<number, OptionType.Int>;
export type StringSetting = BaseSetting<string, OptionType.String>;
export type StringArraySetting = BaseSetting<string[], OptionType.StringArray>;
export type BoolSetting = BaseSetting<boolean, OptionType.Bool>;

/**
 * Apply a quick setting to a value.
 *
 * @param current The current value of the setting.
 * @param qs The quick setting to apply.
 */
export function applyQuickSetting<V extends OptionValueType>(current: V | null, qs: QuickSetting<V>): V | null {
  if (qs.Action === 'replace' || !qs.Action) {
    return deepClone(qs.Value);
  }

  if ((!Array.isArray(current) && current !== null) || !Array.isArray(qs.Value)) {
    console.warn(`Tried to ${qs.Action} quick-setting on non-array type`);
    return current;
  }

  const clone = deepClone(current);
  let missing: any[] = [];

  qs.Value.forEach(val => {
    if (clone.includes(val)) {
      return
    }
    missing.push(val);
  });

  if (qs.Action === 'merge-bottom') {
    return clone.concat(missing) as V;
  }

  return missing.concat(clone) as V;
}

/**
 * Parses the ValidationRegex of a setting and returns a list
 * of supported values.
 *
 * @param s The setting to extract support values from.
 */
export function parseSupportedValues<S extends Setting>(s: S): SettingValueType<S>[] {
  if (!s.ValidationRegex) {
    return [];
  }

  const values = s.ValidationRegex.match(/\w+/gmi);
  const result: SettingValueType<S>[] = [];

  let converter: (s: string) => any;

  switch (s.OptType) {
    case OptionType.Bool:
      converter = s => s === 'true';
      break;
    case OptionType.Int:
      converter = s => +s;
      break;
    case OptionType.String:
    case OptionType.StringArray:
      converter = s => s
      break
  }

  values?.forEach(val => {
    result.push(converter(val))
  });

  return result;
}

/**
 * isDefaultValue checks if value is the settings default value.
 * It supports all available settings type and fallsback to use
 * JSON encoded string comparision (JS JSON.stringify is stable).
 */
export function isDefaultValue<T extends OptionValueType>(value: T | undefined | null, defaultValue: T): boolean {
  if (value === undefined) {
    return true;
  }

  const isObject = typeof value === 'object';
  const isDefault = isObject
    ? JSON.stringify(value) === JSON.stringify(defaultValue)
    : value === defaultValue;

  return isDefault;
}

/**
 * SettingValueType is used to infer the type of a settings from it's default value.
 * Use like this:
 *
 *      validate<S extends Setting>(spec: S, value SettingValueType<S>) { ... }
 */
export type SettingValueType<S extends Setting> = S extends { DefaultValue: infer T } ? T : any;

export type Setting = IntSetting
  | StringSetting
  | StringArraySetting
  | BoolSetting;
