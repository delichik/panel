import { describe, expect, it } from 'vitest';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, extname, relative } from 'node:path';

/**
 * Design-token guard: controls and pages must consume the shared WinUI token /
 * utility layer instead of inventing one-off CSS values. Structural viewport
 * clamps (`calc`, `min()`, `max()`, `dvh`, `vw`, `vh`, `var()`) are allowed;
 * numeric px/rem font sizes, control sizes, radii, shadows and durations are not.
 */
const sourceRoot = join(__dirname, '..');

const allowlist = new Set([
  'icons/index.ts',
  'icons/README.md',
  'design/appearance.ts', // accent seeds + preset gradients are the token definitions
  'styles/main.css',
]);

const deprecatedPatterns: Array<{ pattern: RegExp; message: string }> = [
  { pattern: /text-\[\d/, message: 'use text-xs / text-sm / WinUI type scale instead of arbitrary font sizes' },
  { pattern: /\b(?:w|h|min-w|min-h|max-w|max-h|top|left|right|bottom)-\[\d+(?:\.\d+)?(?:px|rem)\]/, message: 'use the shared spacing scale or semantic utility instead of arbitrary px/rem sizes' },
  { pattern: /\b(?:p|px|py|pt|pb|pl|pr|m|mx|my|mt|mb|ml|mr|gap|gap-x|gap-y|space-x|space-y)-\[\d/, message: 'use the 4px spacing scale (p-1..p-6 / gap-2 ...) instead of arbitrary spacing' },
  { pattern: /rounded-\[/, message: 'use rounded-control / rounded-overlay / rounded-pill / rounded-micro' },
  { pattern: /shadow-\[/, message: 'use shadow-flyout or focus-accent-underline instead of arbitrary shadows' },
  { pattern: /duration-\[|ease-\[/, message: 'use duration-fast / duration-normal / duration-fade and ease-enter / ease-exit / ease-move' },
  { pattern: /backdrop-blur-\[(?!.*var\()/, message: 'use backdrop-blur-flyout / backdrop-blur-subtle' },
  { pattern: /transition-\[/, message: 'use transition-fill / transition-field / transition-state / transition-width' },
  { pattern: /bg-\[var\(/, message: 'use the Tailwind color bridge (bg-accent-strong, bg-control, ...)' },
  { pattern: /rounded-2xl|rounded-xl\b/, message: 'WinUI radii are rounded-control (4px) and rounded-overlay (8px)' },
  { pattern: /bg-background\b/, message: 'page roots stay transparent over Mica; use bg-layer / bg-card' },
  { pattern: /hover:-translate|active:scale/, message: 'WinUI controls never lift or scale on hover/press' },
  { pattern: /@lucide\/vue/, message: 'icons come from the local Fluent layer @/icons' },
];

function stripComments(source: string) {
  return source
    .replace(/<!--[\s\S]*?-->/g, '')
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .split('\n')
    .map((line) => (line.trimStart().startsWith('//') ? '' : line))
    .join('\n');
}

function walk(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    const stat = statSync(path);
    if (stat.isDirectory()) out.push(...walk(path));
    else if (['.vue', '.ts'].includes(extname(path)) && !path.endsWith('.test.ts')) out.push(path);
  }
  return out;
}

describe('WinUI design-token guard', () => {
  it('has no one-off visual values outside the token layer', () => {
    const failures: string[] = [];
    for (const file of walk(sourceRoot)) {
      const relativePath = relative(sourceRoot, file).replace(/\\/g, '/');
      if (allowlist.has(relativePath) || relativePath.startsWith('mocks/')) continue;
      const source = stripComments(readFileSync(file, 'utf8'));
      for (const { pattern, message } of deprecatedPatterns) {
        const match = source.match(pattern);
        if (match) {
          const line = source.slice(0, match.index ?? 0).split('\n').length;
          failures.push(`${relativePath}:${line} matches ${pattern} — ${message}`);
        }
      }
    }
    expect(failures, failures.join('\n')).toEqual([]);
  });

  it('ships the shared utility/token layer used by components', () => {
    const styles = readFileSync(join(sourceRoot, 'styles/main.css'), 'utf8');
    for (const token of [
      '--panel-shell-header',
      '--panel-indicator-width',
      '--panel-control-height',
      '--panel-control-height-sm',
      '--panel-control-height-lg',
      '--panel-touch-target',
      '--panel-field-min-width',
      '--panel-dialog-min-width',
      '--panel-nav-width',
      '--panel-nav-width-compact',
      '--container-content',
      '--container-content-narrow',
      '--breakpoint-wide',
      '--breakpoint-wider',
      '--text-caption',
      '--text-body',
      '--text-body-strong',
      '--text-subtitle',
      '--text-title',
      '--text-title-large',
      '--text-display',
      '--spacing-control',
      '--spacing-control-sm',
      '--spacing-control-lg',
      '--spacing-nav',
      '--spacing-nav-compact',
      '--spacing-touch',
      '--spacing-field',
      '--spacing-dialog',
      '--blur-flyout',
      '--blur-subtle',
      '--spacing-shell-header',
      '--spacing-indicator',
      '--color-accent-strong',
      '--color-accent-stronger',
    ]) {
      expect(styles, `${token} must stay defined`).toContain(token);
    }
    for (const utility of [
      '@utility duration-fast',
      '@utility duration-fade',
      '@utility ease-move',
      '@utility transition-fill',
      '@utility focus-accent-underline',
      '@utility grid-rows-page',
      '@utility grid-rows-shell',
      '@utility grid-cols-main-aside',
      '@utility max-h-list',
      '@utility min-h-touch',
      '@utility min-w-field',
      '@utility min-w-dialog',
      '@utility z-dialog',
      '@utility transition-indicator',
    ]) {
      expect(styles, `${utility} must stay defined`).toContain(utility);
    }
  });
});
