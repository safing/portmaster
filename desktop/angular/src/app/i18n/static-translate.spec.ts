import { getUiLanguage, setUiLanguage, t } from './static-translate';

describe('static-translate', () => {
  beforeEach(() => {
    setUiLanguage('en');
  });

  it('returns English strings by default', () => {
    expect(t('nav_tooltip_monitor')).toBe('Network Activity');
  });

  it('returns Japanese strings after setUiLanguage', () => {
    setUiLanguage('ja');
    expect(t('nav_tooltip_monitor')).toBe('ネットワークアクティビティ');
  });

  it('falls back to English when key is missing in ja', () => {
    setUiLanguage('ja');
    expect(t('nav_test_fallback')).toBe('English Only');
  });

  it('returns fallback argument when key is missing everywhere', () => {
    expect(t('missing.key', 'Fallback')).toBe('Fallback');
  });

  it('returns key when no translation and no fallback', () => {
    expect(t('missing.key')).toBe('missing.key');
  });

  it('getUiLanguage reflects setUiLanguage', () => {
    setUiLanguage('ja');
    expect(getUiLanguage()).toBe('ja');
  });
});
