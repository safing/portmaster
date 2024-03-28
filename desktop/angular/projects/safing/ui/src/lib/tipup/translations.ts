import { InjectionToken } from '@angular/core';

export const SFNG_TIP_UP_CONTENTS = new InjectionToken<HelpTexts<any>>('SfngTipUpContents');
export const SFNG_TIP_UP_ACTION_RUNNER = new InjectionToken<ActionRunner<any>>('SfngTipUpActionRunner')

export interface Button<T> {
  name: string;
  action: T;
  nextKey?: string;
}

export interface TipUp<T> {
  title: string;
  content: string;
  url?: string;
  urlText?: string;
  buttons?: Button<T>[];
  nextKey?: string;
}

export interface HelpTexts<T> {
  [key: string]: TipUp<T>;
}

export interface ActionRunner<T> {
  performAction(action: T): Promise<void>;
}
