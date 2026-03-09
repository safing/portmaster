import { Injectable } from '@angular/core';
import { HelpTexts } from '@safing/ui';
import { LocaleService } from '../services/locale.service';

// Import both language files
import helptextsEn from '../i18n/helptexts.yaml';
import helptextsRu from '../i18n/helptexts.ru.yaml';

export interface LocalizedHelpTexts {
  [lang: string]: HelpTexts<any>;
}

const HELPTEXTS: LocalizedHelpTexts = {
  'en': helptextsEn,
  'ru': helptextsRu
};

@Injectable({
  providedIn: 'root'
})
export class LocalizedHelpTextsService {
  constructor(private readonly localeService: LocaleService) {}

  getHelpTexts(): HelpTexts<any> {
    const lang = this.localeService.getCurrentLanguage();
    return HELPTEXTS[lang] || HELPTEXTS['en'];
  }

  getHelpText(key: string): any {
    const texts = this.getHelpTexts();
    return texts[key];
  }
}

// Factory function for providing help texts based on current language
export function helpTextsFactory(localeService: LocaleService): HelpTexts<any> {
  const lang = localeService.getCurrentLanguage();
  return HELPTEXTS[lang] || HELPTEXTS['en'];
}
