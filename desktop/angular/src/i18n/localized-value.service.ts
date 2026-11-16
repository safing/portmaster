import { Injectable, inject } from '@angular/core';
import { I18nService } from './i18n.service';

/**
 * Domains of backend values that have a stable machine key (ISO code, feature
 * id, enum value, notification event id) and can therefore be localized.
 *
 * Values without such a key (news bodies, GitHub issue titles, free-form
 * messages) deliberately have no domain here and are left untouched.
 */
export type LocalizedValueDomain =
  | 'country'
  | 'scope'
  | 'feature'
  | 'verdict'
  | 'notificationTitle'
  | 'notificationMessage';

const DOMAIN_PREFIX: Record<Exclude<LocalizedValueDomain, 'country'>, string> = {
  scope: 'lc.scope',
  feature: 'lc.feature',
  verdict: 'lc.verdict',
  notificationTitle: 'lc.notification.title',
  notificationMessage: 'lc.notification.message',
};

interface DisplayNamesLike {
  of(code: string): string | undefined;
}

type DisplayNamesCtor = new (locales: string[], options: { type: 'region' }) => DisplayNamesLike;

/**
 * Localizes backend values that carry a stable machine key.
 *
 * Every lookup falls back to the original backend value, so unknown codes,
 * new feature ids or future notification events keep working unchanged.
 */
@Injectable({ providedIn: 'root' })
export class LocalizedValueService {
  private readonly i18n = inject(I18nService);

  /**
   * Resolve a value (or a list of values) for the given domain.
   * Lists are joined with ", " - useful for arrays such as feature_ids.
   */
  localize(
    value: string | number | Array<string | number> | null | undefined,
    domain: LocalizedValueDomain,
    fallback?: string,
  ): string {
    if (value === null || value === undefined || value === '') {
      return fallback ?? '';
    }

    if (Array.isArray(value)) {
      return value
        .map(entry => this.localize(entry, domain, fallback))
        .join(', ');
    }

    if (domain === 'country') {
      return this.countryName(String(value), fallback);
    }

    const key = `${DOMAIN_PREFIX[domain]}.${value}`;
    const translated = this.i18n.t(key);

    // I18nService.t() returns the key itself when no translation exists.
    return translated === key ? (fallback ?? String(value)) : translated;
  }

  /**
   * Resolve an ISO-3166-1 alpha-2 country code to a localized name.
   *
   * Explicit dictionary overrides (see lc.country.*) win, then the platform's
   * Intl.DisplayNames is used for non-English locales. Anything we cannot
   * resolve keeps the name delivered by the backend.
   */
  countryName(code: string, fallback?: string): string {
    if (!code) {
      return fallback ?? '';
    }

    const key = `lc.country.${code}`;
    const override = this.i18n.t(key);
    if (override !== key) {
      return override;
    }

    if (this.i18n.language !== 'en') {
      // Intl.DisplayNames ships in modern webviews; older ones simply fall
      // back to the backend-provided English name below.
      const DisplayNames = (Intl as unknown as { DisplayNames?: DisplayNamesCtor }).DisplayNames;
      if (DisplayNames) {
        try {
          const name = new DisplayNames([this.i18n.language], { type: 'region' }).of(code.toUpperCase());
          if (name && name.toUpperCase() !== code.toUpperCase()) {
            return name;
          }
        } catch (err) {
          console.warn('[i18n] Intl.DisplayNames failed for', code, err);
        }
      }
    }

    return fallback ?? code;
  }
}
