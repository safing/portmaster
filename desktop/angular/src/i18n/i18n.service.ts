import { ApplicationRef, Injectable, inject } from '@angular/core';
import { BehaviorSubject, Observable } from 'rxjs';
import { en } from './translations.en';
import { ru } from './translations.ru';

export type Language = 'en' | 'ru';

const STORAGE_KEY = 'portmaster.ui.language';

const DICTIONARIES: Record<Language, Record<string, string>> = {
  en,
  ru,
};

/**
 * Tiny UI-localisation layer. The language is persisted in localStorage and
 * exposed as an observable so components can refresh on language changes.
 */
@Injectable({ providedIn: 'root' })
export class I18nService {
  private readonly appRef = inject(ApplicationRef);

  private readonly _language$ = new BehaviorSubject<Language>(I18nService.readStoredLanguage());
  readonly language$: Observable<Language> = this._language$.asObservable();

  get language(): Language {
    return this._language$.getValue();
  }

  /** Translate a key, optionally replacing {{name}} placeholders. */
  t(key: string, params?: Record<string, unknown>): string {
    let result = DICTIONARIES[this.language]?.[key] ?? en[key] ?? key;

    if (params) {
      for (const [name, value] of Object.entries(params)) {
        result = result.replace(`{{${name}}}`, String(value));
      }
    }

    return result;
  }

  setLanguage(language: Language): void {
    if (language === this.language) {
      return;
    }

    this._language$.next(language);
    try {
      localStorage.setItem(STORAGE_KEY, language);
    } catch (err) {
      console.warn('[i18n] failed to persist language', err);
    }

    // Impure pipes re-evaluate on every check; schedule a safe global tick so
    // OnPush views (like the navigation) pick up the new strings as well.
    setTimeout(() => {
      try {
        this.appRef.tick();
      } catch (err) {
        console.warn('[i18n] failed to refresh the view', err);
      }
    }, 0);
  }

  private static readStoredLanguage(): Language {
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (stored === 'en' || stored === 'ru') {
        return stored;
      }
    } catch (err) {
      console.warn('[i18n] failed to read stored language', err);
    }
    return 'en';
  }
}
