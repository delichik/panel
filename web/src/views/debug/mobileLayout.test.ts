import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

/**
 * 移动端（360–430px）诊断页契约：
 * - 短分组在 <wide 收成单列（宽屏才并排）；
 * - 按钮组在手机上可换行并占满宽度；
 * - 长标识符（pprof URL、数据库名/表名、任务类型）break-all/换行；
 * - 任务定义表与数据库表用 auto 行高，长内容不被 40px 单行截断；
 * - 页面仍只有 ConsolePage 一个滚动 owner。
 */
const source = readFileSync(resolve(__dirname, './index.vue'), 'utf8');

describe('debug mobile layout contract', () => {
  it('collapses short groups to one column below wide', () => {
    expect(source).toContain('wide:grid-cols-2');
    expect(source).toContain('grid-cols-1');
    // 不再给宽屏两列容器写死固定列数（否则手机上会横向溢出）。
    expect(source).not.toMatch(/class="grid gap-4 grid-cols-2/);
  });

  it('wraps button groups and makes page actions full width on phones', () => {
    expect((source.match(/max-sm:w-full/g) ?? []).length).toBeGreaterThanOrEqual(3);
    expect(source).toMatch(/flex flex-wrap items-center justify-between/);
  });

  it('breaks long identifiers and keeps definition/database tables multi-line', () => {
    expect(source).toContain('break-all');
    expect((source.match(/row-height="auto"/g) ?? []).length).toBeGreaterThanOrEqual(2);
  });

  it('keeps a single ConsolePage scroll owner (no extra page-level scroll wrapper)', () => {
    expect(source).toContain('<ConsolePage');
    expect(source).not.toContain('WorkspacePage');
    expect(source).not.toContain('min-h-150');
  });
});
