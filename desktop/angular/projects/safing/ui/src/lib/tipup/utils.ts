import { InjectionToken } from "@angular/core";

export const TIPUP_TOKEN = new InjectionToken<string>('TipUPJSONToken');

export interface SfngTipUpPlacement {
  origin?: 'left' | 'right';
  offset?: number;
}
