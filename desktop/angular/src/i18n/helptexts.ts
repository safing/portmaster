import HelptextsEN from 'js-yaml-loader!./helptexts.yaml';
import HelptextsRU from 'js-yaml-loader!./helptexts.ru.yaml';
import { I18nService, Language } from './i18n.service';

/**
 * All localized tip-up / helptext dictionaries. The keys are identical across
 * languages so switching at runtime is just a matter of picking a dictionary.
 */
export const HELP_TEXTS: Record<Language, typeof HelptextsEN> = {
  en: HelptextsEN,
  ru: HelptextsRU,
};

/**
 * Factory for the SFNG_TIP_UP_CONTENTS token used by @safing/ui.
 *
 * The UI library reads `contents[key]` every time a tip-up is opened, so a
 * Proxy lets us serve the dictionary matching the currently selected language
 * without changing the library or requiring an application restart.
 */
export function createHelpTexts(i18n: I18nService): typeof HelptextsEN {
  return new Proxy({} as typeof HelptextsEN, {
    get(_target, key: string | symbol) {
      if (typeof key !== 'string') {
        return undefined;
      }
      const current = HELP_TEXTS[i18n.language] ?? HELP_TEXTS.en;
      return (current as Record<string, unknown>)[key] ??
        (HELP_TEXTS.en as Record<string, unknown>)[key];
    },
    has(_target, key: string | symbol) {
      return typeof key === 'string' && key in HELP_TEXTS.en;
    },
    ownKeys() {
      return Reflect.ownKeys(HELP_TEXTS.en);
    },
    getOwnPropertyDescriptor() {
      return { enumerable: true, configurable: true };
    },
  });
}
