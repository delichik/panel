import { describe, expect, it } from 'vitest';
import { messages } from './index';

const locales = ['en', 'zh-CN'] as const;

describe('i18n message consistency', () => {
  it('en and zh-CN expose the same key sets', () => {
    const enKeys = Object.keys(messages.en).sort();
    const zhKeys = Object.keys(messages['zh-CN']).sort();
    expect(zhKeys).toEqual(enKeys);
  });

  it('does not leave untranslated Chinese values in en', () => {
    const cjk = /[\u4e00-\u9fff]/;
    const offenders = Object.entries(messages.en).filter(([, value]) => cjk.test(value));
    expect(offenders).toEqual([]);
  });

  it('keeps every message non-empty', () => {
    for (const locale of locales) {
      const empty = Object.entries(messages[locale]).filter(([, value]) => !value.trim());
      expect(empty, locale).toEqual([]);
    }
  });
});