import { Pipe, PipeTransform, inject } from '@angular/core';
import { I18nService, Language } from './i18n.service';

/**
 * Usage: {{ 'settings.title' | t }} or {{ 'xray.connectedTo' | t: { node: name } }}
 *
 * The pipe is pure: Angular only calls transform when one of the arguments
 * changes. Templates pass `('' | i18nVersion)` as the final argument so a
 * language switch invalidates the result, while ordinary change detection
 * reuses the cached string instead of doing 600+ dictionary lookups.
 */
@Pipe({ name: 't' })
export class TranslatePipe implements PipeTransform {
  private readonly i18n = inject(I18nService);

  private readonly cache = new Map<string, string>();
  private cacheLanguage: Language | null = null;

  // `version` is only used by Angular to decide when to re-run this pure pipe;
  // the cache itself is keyed off the service so it can never serve stale text.
  transform(key: string, params?: Record<string, unknown>, _version?: Language): string {
    const language = this.i18n.language;
    if (this.cacheLanguage !== language) {
      this.cache.clear();
      this.cacheLanguage = language;
    }

    // A params object literal is re-created on every check, so include its
    // values - not its identity - in the cache key.
    const cacheKey = params ? `${key}|${this.serializeParams(params)}` : key;
    let result = this.cache.get(cacheKey);
    if (result === undefined) {
      result = this.i18n.t(key, params);
      this.cache.set(cacheKey, result);
    }

    return result;
  }

  private serializeParams(params: Record<string, unknown>): string {
    return Object.keys(params)
      .sort()
      .map(name => `${name}=${String(params[name])}`)
      .join('&');
  }
}
