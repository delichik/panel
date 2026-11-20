import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const source = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');

/**
 * 编辑器检查结果弹窗契约（task-61）：
 * - 删除应用/设施编辑器的右侧 summary + 检查结果常驻面板，主区单列全宽；
 * - 保存流程内部仍走 preview 取 token；blocking diagnostics 时打开共享 Dialog
 *   （标题「检查结果」= applicationsPage.diagnostics，说明 validationFoundDialog，
 *   列表复用 ApplicationDiagnosticsList），不再 notify/右侧查看；
 * - 成功保存仍 toast；关闭弹窗停留编辑器。
 */
describe('applications editor validation dialog', () => {
  it('removes the summary/diagnostics aside and its diff/diagnostic-only bindings', () => {
    expect(source).not.toContain('<aside');
    expect(source).not.toContain('xl:grid-cols-[minmax(0,1fr)_320px]');
    expect(source).not.toContain('xl:sticky xl:top-0');
    // 不再引用已删除的摘要/diff 计算与文案。
    for (const removed of ['appDiff', 'facilityRecordDiff', 'sourceSummary', 'previewAdded', 'previewChanged', 'previewRemoved', 'createSummary', 'changeSummary', 'facilityRecordSummary', 'noDiagnostics']) {
      expect(source, removed).not.toContain(removed);
    }
    // 对应 import 也已删除。
    expect(source).not.toContain('diffApplications');
    expect(source).not.toContain('diffFacilityRecord');
    expect(source).not.toContain("import PropertyList from");
  });

  it('opens the shared dialog on blocking diagnostics for both editors', () => {
    const blocking = source.match(/if \(hasBlockingDiagnostic\([\s\S]{0,120}?editorDiagnosticsOpen\.value = true;/g) ?? [];
    expect(blocking).toHaveLength(2);
    // 不再用 toast 提示“检查结果/右侧查看”。
    expect(source).not.toMatch(/validationFound['"`]/);
    expect(source).toContain("t('applicationsPage.validationFoundDialog')");
  });

  it('renders the dialog with the diagnostics list and closes back into the editor', () => {
    const start = source.indexOf(':open="editorDiagnosticsOpen"');
    expect(start).toBeGreaterThan(-1);
    const dialog = source.slice(start, source.indexOf('</Dialog>', start));
    expect(dialog).toContain(":title=\"t('applicationsPage.diagnostics')\"");
    expect(dialog).toContain(":description=\"t('applicationsPage.validationFoundDialog')\"");
    expect(dialog).toContain('<ApplicationDiagnosticsList :items="editorDiagnostics" />');
    expect(dialog).toContain('editorDiagnosticsOpen = false');
    // 编辑器内部诊断源按当前编辑器选择。
    expect(source).toContain('const editorDiagnostics = computed(() => (isFacilityEditor.value ? facilityDiagnostics.value : diagnostics.value));');
  });

  it('keeps the success toast and the save-ready footer wording', () => {
    expect(source).toContain("t('applicationsPage.committedAndApplied')");
    expect(source).toContain("t('applicationsPage.committed')");
    expect(source.match(/applicationsPage\.readyToCommit/g)?.length ?? 0).toBeGreaterThanOrEqual(2);
  });
});
