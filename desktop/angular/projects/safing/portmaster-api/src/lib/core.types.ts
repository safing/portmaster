import { TrackByFunction } from '@angular/core';

export enum SecurityLevel {
  Off = 0,
  Normal = 1,
  High = 2,
  Extreme = 4,
}

export enum RiskLevel {
  Off = 'off',
  Auto = 'auto',
  Low = 'low',
  Medium = 'medium',
  High = 'high'
}

/** Interface capturing any object that has an ID member. */
export interface Identifyable {
  ID: string | number;
}

/** A TrackByFunction for all Identifyable objects. */
export const trackById: TrackByFunction<Identifyable> = (_: number, obj: Identifyable) => {
  return obj.ID;
}

export function getEnumKey(enumLike: any, value: string | number): string {
  if (typeof value === 'string') {
    return value.toLowerCase()
  }

  return (enumLike[value] as string).toLowerCase()
}
