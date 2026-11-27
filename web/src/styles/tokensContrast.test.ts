import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { ACCENT_PRESETS } from '@/design/appearance';

interface Rgba { r: number; g: number; b: number; a: number; }

const mainStyles = readFileSync(resolve(__dirname, 'main.css'), 'utf8');

function parseBlock(selector: string): Record<string, string> {
  const start = mainStyles.indexOf(selector);
  expect(start, `${selector} block must exist`).toBeGreaterThanOrEqual(0);
  const blockStart = mainStyles.indexOf('{', start);
  const blockEnd = mainStyles.indexOf('}', blockStart);
  const block = mainStyles.slice(blockStart + 1, blockEnd);
  const values: Record<string, string> = {};
  for (const match of block.matchAll(/(--[\w-]+):\s*([^;]+);/g)) {
    values[match[1]] = match[2].trim();
  }
  return values;
}

function parseColor(value: string): Rgba {
  const hex = value.match(/^#([0-9a-f]{6})$/i);
  if (hex) {
    const number = parseInt(hex[1], 16);
    return { r: (number >> 16) & 255, g: (number >> 8) & 255, b: number & 255, a: 1 };
  }
  const rgba = value.match(/^rgba?\(\s*([\d.]+)\s*,\s*([\d.]+)\s*,\s*([\d.]+)(?:\s*,\s*([\d.]+))?\s*\)$/i);
  if (rgba) {
    return { r: Number(rgba[1]), g: Number(rgba[2]), b: Number(rgba[3]), a: rgba[4] === undefined ? 1 : Number(rgba[4]) };
  }
  throw new Error(`Unsupported color token: ${value}`);
}

/** Composite a translucent surface over an opaque background (Mica/window base). */
function over(top: Rgba, bottom: Rgba): Rgba {
  return {
    r: top.r * top.a + bottom.r * (1 - top.a),
    g: top.g * top.a + bottom.g * (1 - top.a),
    b: top.b * top.a + bottom.b * (1 - top.a),
    a: 1,
  };
}

function channel(value: number) {
  const normalized = value / 255;
  return normalized <= 0.03928 ? normalized / 12.92 : ((normalized + 0.055) / 1.055) ** 2.4;
}

function luminance(color: Rgba) {
  return 0.2126 * channel(color.r) + 0.7152 * channel(color.g) + 0.0722 * channel(color.b);
}

function contrast(a: Rgba, b: Rgba) {
  const l1 = luminance(a);
  const l2 = luminance(b);
  const [hi, lo] = l1 >= l2 ? [l1, l2] : [l2, l1];
  return (hi + 0.05) / (lo + 0.05);
}

function mixWithWhite(hex: string, whiteWeightPercent: number): Rgba {
  const number = parseInt(hex.slice(1), 16);
  const weight = whiteWeightPercent / 100;
  return {
    r: ((number >> 16) & 255) * weight + 255 * (1 - weight),
    g: ((number >> 8) & 255) * weight + 255 * (1 - weight),
    b: (number & 255) * weight + 255 * (1 - weight),
    a: 1,
  };
}

function mixWithBlack(hex: string, seedWeightPercent: number): Rgba {
  const number = parseInt(hex.slice(1), 16);
  const weight = seedWeightPercent / 100;
  return {
    r: ((number >> 16) & 255) * weight,
    g: ((number >> 8) & 255) * weight,
    b: (number & 255) * weight,
    a: 1,
  };
}

/** A seed tint composited over a base surface (accent-selected-bg semantics). */
function tintOver(hex: string, seedWeight: number, base: Rgba): Rgba {
  const number = parseInt(hex.slice(1), 16);
  return {
    r: ((number >> 16) & 255) * seedWeight + base.r * (1 - seedWeight),
    g: ((number >> 8) & 255) * seedWeight + base.g * (1 - seedWeight),
    b: (number & 255) * seedWeight + base.b * (1 - seedWeight),
    a: 1,
  };
}

describe('WinUI token contrast (WCAG AA)', () => {
  const light = parseBlock(':root {');
  const dark = parseBlock(":root[data-theme='dark'] {");

  it('primary and secondary text stay readable on the Mica/fallback surfaces in both themes', () => {
    for (const [name, tokens] of [['light', light], ['dark', dark]] as const) {
      const bg = parseColor(tokens['--panel-bg']);
      const card = over(parseColor(tokens['--panel-card']), bg);
      const control = over(parseColor(tokens['--panel-control-fill']), bg);
      const text = parseColor(tokens['--panel-text']);
      const secondary = parseColor(tokens['--panel-text-secondary']);

      for (const [surfaceName, surface] of [['bg', bg], ['card', card], ['control', control]] as const) {
        expect(contrast(text, surface), `${name}: primary text on ${surfaceName}`).toBeGreaterThanOrEqual(4.5);
        expect(contrast(secondary, surface), `${name}: secondary text on ${surfaceName}`).toBeGreaterThanOrEqual(4.5);
      }
    }
  });

  it('muted/caption text meets AA on the Mica/fallback surfaces in both themes', () => {
    for (const [name, tokens] of [['light', light], ['dark', dark]] as const) {
      const bg = parseColor(tokens['--panel-bg']);
      const card = over(parseColor(tokens['--panel-card']), bg);
      const control = over(parseColor(tokens['--panel-control-fill']), bg);
      const muted = parseColor(tokens['--panel-text-muted']);
      for (const [surfaceName, surface] of [['bg', bg], ['card', card], ['control', control]] as const) {
        expect(contrast(muted, surface), `${name}: muted text on ${surfaceName}`).toBeGreaterThanOrEqual(4.5);
      }
    }
  });

  it('accent text keeps AA on card and selected fill in both themes', () => {
    for (const accent of ACCENT_PRESETS) {
      const lightText = mixWithBlack(accent.seed, 72);
      const darkText = mixWithWhite(accent.seed, 45);
      const lightSelected = tintOver(accent.seed, 0.12, parseColor('#ffffff'));
      const darkSelected = tintOver(accent.seed, 0.12, parseColor('#2b2b2b'));
      expect(contrast(lightText, parseColor('#ffffff')), `${accent.id} light accent text on card`).toBeGreaterThanOrEqual(4.5);
      expect(contrast(lightText, lightSelected), `${accent.id} light accent text on selected`).toBeGreaterThanOrEqual(4.5);
      expect(contrast(darkText, parseColor('#2b2b2b')), `${accent.id} dark accent text on card`).toBeGreaterThanOrEqual(4.5);
      expect(contrast(darkText, darkSelected), `${accent.id} dark accent text on selected`).toBeGreaterThanOrEqual(4.5);
    }
  });

  it('every accent preset keeps a readable in-button foreground (light and dark theme)', () => {
    for (const accent of ACCENT_PRESETS) {
      // Light theme: white label on the seed fill.
      expect(contrast(parseColor(accent.seed), parseColor('#ffffff')), `${accent.id} white-on-seed`).toBeGreaterThanOrEqual(4.5);
      // Dark theme: near-black label on the lightened accent fill (color-mix 45% seed + white).
      expect(contrast(mixWithWhite(accent.seed, 45), parseColor('#1a1a1a')), `${accent.id} dark-on-fill`).toBeGreaterThanOrEqual(4.5);
      // Accent text used for links/selected labels (color-mix 72% seed + black) on white/card.
      expect(contrast(mixWithBlack(accent.seed, 72), parseColor('#ffffff')), `${accent.id} accent-text on light`).toBeGreaterThanOrEqual(4.5);
    }
  });
});
