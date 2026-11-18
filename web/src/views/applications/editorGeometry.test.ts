import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const viewSource = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');

/**
 * 编辑器几何契约（修复批次 1 + 内容列统一 + 单列主区）：
 * - 应用编辑器 / 设施记录配置统一走 ConsolePage width="content"（1200，与所有页面一致；
 *   表单可读宽度由内部栅格控制，不用页面宽度收窄）；
 * - 主区单列全宽：不再保留右侧摘要/检查结果 aside，也不再有 xl 双列/sticky；
 * - ConsolePage 是唯一页面滚动/padding owner（编辑器 body-scroll=false、无 EditorPage 以外的页面 padding）。
 */
describe('applications editor geometry', () => {
  it('uses the shared 1200 content column for both editors', () => {
    // 应用编辑器 + 设施配置页都显式声明 content（默认值也是 content，但显式写出避免回退到窄列）。
    expect(viewSource.match(/width="content"/g)?.length ?? 0).toBeGreaterThanOrEqual(2);
    expect(viewSource).not.toContain('width="narrow"');
    expect(viewSource).not.toContain(':width=');
  });

  it('keeps both editors single-column without a summary/diagnostics aside', () => {
    expect(viewSource).not.toContain('xl:grid-cols-[minmax(0,1fr)_320px]');
    expect(viewSource).not.toContain('xl:sticky xl:top-0');
    expect(viewSource).not.toContain('<aside');
    // 两个编辑器主区都是单列 grid。
    expect(viewSource.match(/class="relative grid min-w-0 content-start gap-4"/g) ?? []).toHaveLength(2);
  });

  it('keeps both editors on the ConsolePage single-scroll contract without a third padding layer', () => {
    // 两个编辑器都关闭 ConsolePage 滚动，由 EditorPage 成为唯一滚动 owner。
    expect(viewSource.match(/:body-scroll="false"/g)?.length).toBeGreaterThanOrEqual(2);
    expect(viewSource).toContain('<EditorPage>');
    // 页面级 padding 只允许出现在 ConsolePage 模板里；本页不得再包 p-4/p-6 页面壳。
    expect(viewSource).not.toMatch(/<div[^>]*class="[^"]*\bp-[456]\b[^"]*"[^>]*>\s*<EditorPage>/);
  });
});
