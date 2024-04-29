import { BaseSetting, OptionValueType, SettingValueType } from './config.types';
import { SecurityLevel } from './core.types';
import { Record } from './portapi.types';

export interface ConfigMap {
  [key: string]: ConfigObject;
}

export type ConfigObject = OptionValueType | ConfigMap;

export interface FlatConfigObject {
  [key: string]: OptionValueType;
}


export interface LayeredProfile extends Record {
  // LayerIDs is a list of all profiles that are used
  // by this layered profile. Profiles are evaluated in
  // order.
  LayerIDs: string[];

  // The current revision counter of the layered profile.
  RevisionCounter: number;
}

export enum FingerprintType {
  Tag = 'tag',
  Cmdline = 'cmdline',
  Env = 'env',
  Path = 'path',
}

export enum FingerpringOperation {
  Equal = 'equals',
  Prefix = 'prefix',
  Regex = 'regex',
}

export interface Fingerprint {
  Type: FingerprintType;
  Key: string;
  Operation: FingerpringOperation;
  Value: string;
}

export interface TagDescription {
  ID: string;
  Name: string;
  Description: string;
}

export interface Icon {
  Type: '' | 'database' | 'path' | 'api';
  Source: '' | 'user' | 'import' | 'core' | 'ui';
  Value: string;
}

export interface AppProfile extends Record {
  ID: string;
  LinkedPath: string; // deprecated
  PresentationPath: string;
  Fingerprints: Fingerprint[];
  Created: number;
  LastEdited: number;
  Config?: ConfigMap;
  Description: string;
  Warning: string;
  WarningLastUpdated: string;
  Homepage: string;
  Icons: Icon[];
  Name: string;
  Internal: boolean;
  SecurityLevel: SecurityLevel;
  Source: 'local';
}

// flattenProfileConfig returns a flat version of a nested ConfigMap where each property
// can be used as the database key for the associated setting.
export function flattenProfileConfig(
  p?: ConfigMap,
  prefix = ''
): FlatConfigObject {
  if (p === null || p === undefined) {
    return {}
  }

  let result: FlatConfigObject = {};

  Object.keys(p).forEach((key) => {
    const childPrefix = prefix === '' ? key : `${prefix}/${key}`;

    const prop = p[key];

    if (isConfigMap(prop)) {
      const flattened = flattenProfileConfig(prop, childPrefix);
      result = mergeObjects(result, flattened);
      return;
    }

    result[childPrefix] = prop;
  });

  return result;
}

/**
 * Returns the current value (or null) of a setting stored in a config
 * map by path.
 *
 * @param obj The ConfigMap object
 * @param path  The path of the setting separated by foward slashes.
 */
export function getAppSetting<T extends OptionValueType>(
  obj: ConfigMap | null | undefined,
  path: string
): T | null {
  if (obj === null || obj === undefined) {
    return null
  }

  const parts = path.split('/');

  let iter = obj;
  for (let idx = 0; idx < parts.length; idx++) {
    const propName = parts[idx];

    if (iter[propName] === undefined) {
      return null;
    }

    const value = iter[propName];
    if (idx === parts.length - 1) {
      return value as T;
    }

    if (!isConfigMap(value)) {
      return null;
    }

    iter = value;
  }
  return null;
}

export function getActualValue<S extends BaseSetting<any, any>>(
  s: S
): SettingValueType<S> {
  if (s.Value !== undefined) {
    return s.Value;
  }
  if (s.GlobalDefault !== undefined) {
    return s.GlobalDefault;
  }
  return s.DefaultValue;
}

/**
 * Sets the value of a settings inside the nested config object.
 *
 * @param obj THe config object
 * @param path  The path of the setting
 * @param value The new value to set.
 */
export function setAppSetting(obj: ConfigObject, path: string, value: any) {
  const parts = path.split('/');
  if (typeof obj !== 'object' || Array.isArray(obj)) {
    return;
  }

  let iter = obj;
  for (let idx = 0; idx < parts.length; idx++) {
    const propName = parts[idx];

    if (idx === parts.length - 1) {
      if (value === undefined) {
        delete iter[propName];
      } else {
        iter[propName] = value;
      }
      return;
    }

    if (iter[propName] === undefined) {
      iter[propName] = {};
    }

    iter = iter[propName] as ConfigMap;
  }
}

/** Typeguard to ensure v is a ConfigMap */
function isConfigMap(v: any): v is ConfigMap {
  return typeof v === 'object' && !Array.isArray(v);
}

/**
 * Returns a new flat-config object that contains values from both
 * parameters.
 *
 * @param a The first config object
 * @param b The second config object
 */
function mergeObjects(
  a: FlatConfigObject,
  b: FlatConfigObject
): FlatConfigObject {
  var res: FlatConfigObject = {};
  Object.keys(a).forEach((key) => {
    res[key] = a[key];
  });
  Object.keys(b).forEach((key) => {
    res[key] = b[key];
  });
  return res;
}
