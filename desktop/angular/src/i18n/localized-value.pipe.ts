import { Pipe, PipeTransform, inject } from '@angular/core';
import { Language } from './i18n.service';
import { LocalizedValueDomain, LocalizedValueService } from './localized-value.service';

/**
 * Localizes a backend value that has a stable machine key:
 *
 *   {{ conn.country | localizedValue:'country' }}
 *   {{ featureIds    | localizedValue:'feature' }}
 *   {{ conn.verdict  | localizedValue:'verdict':verdict[conn.verdict] }}
 *
 * The optional fallback is returned for unknown values. The pipe is pure; a
 * language switch is signalled through the `version` argument supplied by the
 * `i18nVersion` pipe, so the (potentially expensive) lookup only runs when the
 * value or the language actually changes.
 */
@Pipe({ name: 'localizedValue' })
export class LocalizedValuePipe implements PipeTransform {
  private readonly localized = inject(LocalizedValueService);

  transform(
    value: string | number | Array<string | number> | null | undefined,
    domain: LocalizedValueDomain,
    fallback?: string,
    _version?: Language,
  ): string {
    return this.localized.localize(value, domain, fallback);
  }
}
