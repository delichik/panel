import { readdirSync, readFileSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

/**
 * 反馈渠道规范守卫（对应 UI-SHELL-032 / UI-SHELL-056 / UI-SHELL-065）。
 *
 * 右上角 toast 只承载两类内容：事件通知（EventNotificationBridge）与用户主动操作成功
 * （useSuccessToast）。任何报错都必须走阻塞操作的全局弹窗（useErrorDialog）或页面就地
 * 错误态，不得回到右上角；`useErrorToast` 已被删除，任何残留都会让编译期/本守卫失败。
 */
const SRC_ROOT = resolve(__dirname, '..', '..');

function sourceFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const full = join(directory, entry.name);
    if (entry.isDirectory()) return sourceFiles(full);
    return /\.(ts|vue)$/.test(entry.name) ? [full] : [];
  });
}

/** 允许直接访问 toast 基础设施的文件：toast 自身、mock、测试与事件通知桥。 */
const ALLOWED = [
  /^components\/ui\/toast/,
  /^components\/ui\/ToastProvider\.vue$/,
  /^mocks\//,
  /\.test\.ts$/,
  /^components\/shell\/EventNotificationBridge\.vue$/,
];

const businessFiles = sourceFiles(SRC_ROOT)
  .map((file) => ({ file, path: relative(SRC_ROOT, file).replace(/\\/g, '/') }))
  .filter(({ path }) => !ALLOWED.some((pattern) => pattern.test(path)));

describe('toast feedback policy', () => {
  it('finds business sources to scan', () => {
    // 防止守卫因路径解析错误而空扫（例如在错误的工作目录下运行）。
    expect(businessFiles.length).toBeGreaterThan(100);
  });

  it('has no useErrorToast outside the toast infrastructure', () => {
    const offenders = businessFiles
      .filter(({ file }) => readFileSync(file, 'utf8').includes('useErrorToast'))
      .map(({ path }) => path);
    expect(offenders).toEqual([]);
  });

  it('keeps useToast itself limited to infrastructure, mocks and the event bridge', () => {
    // 业务代码只能用 useSuccessToast（成功提示）；报错走 useErrorDialog 或就地错误态。
    const offenders = businessFiles
      .filter(({ file }) => /\buseToast\b/.test(readFileSync(file, 'utf8')))
      .map(({ path }) => path);
    expect(offenders).toEqual([]);
  });
});
