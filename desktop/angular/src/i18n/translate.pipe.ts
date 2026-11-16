import { Pipe, PipeTransform } from '@angular/core';
import { I18nService } from './i18n.service';

/**
 * Usage: {{ 'settings.title' | t }} or {{ 'xray.connectedTo' | t: { node: name } }}
 * Impure so that a language switch immediately refreshes the rendered string.
 */
@Pipe({ name: 't', pure: false })
export class TranslatePipe implements PipeTransform {
  constructor(private readonly i18n: I18nService) { }

  transform(key: string, params?: Record<string, unknown>): string {
    return this.i18n.t(key, params);
  }
}
