import { Injectable } from '@angular/core';
import { TranslateService } from '@ngx-translate/core';
import { BehaviorSubject } from 'rxjs';

export interface Language {
  code: string;
  name: string;
  nativeName: string;
}

export const AVAILABLE_LANGUAGES: Language[] = [
  { code: 'en', name: 'English', nativeName: 'English' },
  { code: 'ru', name: 'Russian', nativeName: 'Русский' }
];

@Injectable({
  providedIn: 'root'
})
export class LocaleService {
  private readonly currentLang$ = new BehaviorSubject<string>('en');
  
  readonly languages = AVAILABLE_LANGUAGES;
  readonly currentLanguage$ = this.currentLang$.asObservable();

  constructor(private readonly translate: TranslateService) {
    // Set available languages
    this.translate.addLangs(AVAILABLE_LANGUAGES.map(l => l.code));
    this.translate.setDefaultLang('en');
    
    // Try to use browser language or stored preference
    const storedLang = localStorage.getItem('portmaster-language');
    const browserLang = this.translate.getBrowserLang();
    
    let langToUse = 'en';
    if (storedLang && this.isSupported(storedLang)) {
      langToUse = storedLang;
    } else if (browserLang && this.isSupported(browserLang)) {
      langToUse = browserLang;
    }
    
    this.setLanguage(langToUse);
  }

  setLanguage(langCode: string): void {
    if (this.isSupported(langCode)) {
      this.translate.use(langCode);
      this.currentLang$.next(langCode);
      localStorage.setItem('portmaster-language', langCode);
      document.documentElement.lang = langCode;
    }
  }

  getCurrentLanguage(): string {
    return this.currentLang$.value;
  }

  getLanguageByCode(code: string): Language | undefined {
    return AVAILABLE_LANGUAGES.find(l => l.code === code);
  }

  private isSupported(langCode: string): boolean {
    return AVAILABLE_LANGUAGES.some(l => l.code === langCode);
  }
}
