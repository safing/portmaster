import { Pipe, PipeTransform, inject } from '@angular/core';
import { LocalizedValueDomain, LocalizedValueService } from './localized-value.service';

/**
 * Localizes a backend value that has a stable machine key:
 *
 *   {{ conn.country | localizedValue:'country' }}
 *   {{ featureIds    | localizedValue:'feature' }}
 *   {{ conn.verdict  | localizedValue:'verdict':verdict[conn.verdict] }}
 *
 * The optional fallback is returned for unknown values. The pipe is impure so
 * a language switch refreshes the rendered value immediately.
 */
@Pipe({ name: 'localizedValue', pure: false })
export class LocalizedValuePipe implements PipeTransform {
  private readonly localized = inject(LocalizedValueService);

  transform(
    value: string | number | Array<string | number> | null | undefined,
    domain: LocalizedValueDomain,
    fallback?: string,
  ): string {
    return this.localized.localize(value, domain, fallback);
  }
}
