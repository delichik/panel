import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const mainStyles = readFileSync(resolve(__dirname, 'main.css'), 'utf8');
const shellSource = readFileSync(resolve(__dirname, '../components/shell/AppShell.vue'), 'utf8');
const navListSource = readFileSync(resolve(__dirname, '../components/shell/NavList.vue'), 'utf8');
const consolePageSource = readFileSync(resolve(__dirname, '../components/templates/ConsolePage.vue'), 'utf8');
const editorPageSource = readFileSync(resolve(__dirname, '../components/templates/EditorPage.vue'), 'utf8');
const dialogSource = readFileSync(resolve(__dirname, '../components/ui/Dialog.vue'), 'utf8');
const dropdownSource = readFileSync(resolve(__dirname, '../components/ui/Dropdown.vue'), 'utf8');
const routerSource = readFileSync(resolve(__dirname, '../router/index.ts'), 'utf8');

describe('new frontend foundation', () => {
  it('uses Tailwind and self-owned semantic tokens', () => {
    expect(mainStyles).toContain('@import "tailwindcss"');
    expect(mainStyles).toContain('--panel-bg:');
    expect(mainStyles).toContain('--panel-primary:');
    expect(mainStyles).toContain('--panel-success-bg:');
    expect(mainStyles).not.toContain('--v-theme');
  });

  it('keeps desktop shell scrolling constrained to internal regions', () => {
    expect(mainStyles).toMatch(/body\s*\{[^}]*overflow:\s*hidden;/s);
    expect(shellSource).toContain('lg:h-dvh lg:min-h-0 lg:overflow-hidden');
    expect(shellSource).toContain('lg:grid-rows-[56px_minmax(0,1fr)]');
    expect(shellSource).toContain('overflow-hidden');
  });

  it('does not route every page through the retired collection shell', () => {
    expect(routerSource).not.toContain('CollectionPage');
    expect(routerSource).toContain('@/views/servers/index.vue');
    expect(routerSource).toContain('@/views/security/index.vue');
    expect(routerSource).toContain('@/views/resources/index.vue');
    expect(routerSource).toContain('@/views/applications/index.vue');
  });
});

describe('motion foundation', () => {
  it('lets entrance animations release hover and press transforms', () => {
    // fill-mode: both 会在动画结束后继续压制 hover/active 的 transform，
    // 必须使用 backwards，让元素回到自身计算样式。
    expect(mainStyles).toContain('panel-motion-enter var(--panel-motion-duration-slow) var(--panel-motion-ease-emphasized) backwards');
    expect(mainStyles).not.toMatch(/panel-motion-enter[^;]*\bboth\b/);
    expect(mainStyles).toContain('.motion-control:not(:disabled):hover');
  });

  it('ships the shared collapse, detail-swap and indeterminate progress primitives', () => {
    expect(mainStyles).toContain('.shell-label');
    expect(mainStyles).toContain('.detail-swap-enter-active');
    expect(mainStyles).toContain('.detail-swap-enter-from');
    expect(mainStyles).toContain('@keyframes panel-motion-progress');
  });

  it('disables the new motion in the reduced-motion downgrade', () => {
    expect(mainStyles).toMatch(/\.detail-swap-enter-from,/);
    expect(mainStyles).toMatch(/\.motion-skeleton,\s*\.motion-progress\s*\{\s*animation: none !important;/);
  });

  it('keeps custom hover feedback off touch devices and enables fast taps', () => {
    // 触屏上半秒后的 :hover 会“粘”住；自有 motion 类必须和 Tailwind 的 hover: 变体一样受 (hover: hover) 保护。
    expect(mainStyles).toMatch(/@media \(hover: hover\) \{[\s\S]*?\.motion-control:not\(:disabled\):hover/);
    expect(mainStyles).toContain('touch-action: manipulation');
    // 选中态阴影与 hover 无关，触屏下仍要保留。
    expect(mainStyles).toMatch(/\.motion-list-item\[aria-current='true'\]/);
  });

  it('enlarges the narrow-screen navigation rows to a touch-friendly height', () => {
    expect(navListSource).toContain('max-lg:h-11');
    expect(shellSource).toContain('max-lg:size-11');
  });
});

describe('responsive scrolling exception', () => {
  it('restores page-level scrolling below the desktop breakpoint', () => {
    expect(mainStyles).toMatch(/@media \(max-width:\s*1023\.98px\)\s*\{[\s\S]*body\s*\{[^}]*overflow:\s*auto;/);
    expect(shellSource).toContain('overflow-visible lg:overflow-hidden');
    expect(consolePageSource).toContain('max-lg:overflow-visible');
    expect(editorPageSource).toContain('max-lg:overflow-visible');
    expect(dialogSource).toContain('min-h-0 overflow-auto');
    expect(dropdownSource).toContain('<Teleport to="body">');
    expect(dropdownSource).toContain("position: 'fixed'");
  });
});
