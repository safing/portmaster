import { Pipe, PipeTransform, inject } from '@angular/core';
import { I18nService, Language } from './i18n.service';

/**
 * Returns the currently active language as a cache-busting "version" value.
 *
 * The localisation pipes are pure so they do not re-run on every change
 * detection pass. Passing `('' | i18nVersion)` as their last argument gives
 * Angular a value that changes exactly once per language switch, which makes
 * the pure pipes recompute without an impure lookup on every CD cycle.
 *
 * This pipe is impure by design, but its transform is only a property read -
 * unlike the real translation work it is effectively free.
 */
@Pipe({ name: 'i18nVersion', pure: false })
export class I18nVersionPipe implements PipeTransform {
  private readonly i18n = inject(I18nService);

  transform(_value?: unknown): Language {
    return this.i18n.language;
  }
}
