import * as en from '../../assets/i18n/en.json';
import * as ja from '../../assets/i18n/ja.json';

export type UiLang = 'en' | 'ja';

const dictionaries: Record<UiLang, Record<string, unknown>> = { en, ja };
let currentLang: UiLang = 'en';

export function setUiLanguage(lang: UiLang): void {
  currentLang = lang;
}

export function getUiLanguage(): UiLang {
  return currentLang;
}

export function t(key: string, fallback?: string): string {
  const value = lookup(dictionaries[currentLang], key)
    ?? lookup(dictionaries.en, key);
  if (typeof value === 'string') {
    return value;
  }
  return fallback ?? key;
}

function lookup(dict: Record<string, unknown>, key: string): unknown {
  return dict[key];
}
