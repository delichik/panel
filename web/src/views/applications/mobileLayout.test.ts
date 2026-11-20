import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const source = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');
const catalogSource = readFileSync(resolve(__dirname, 'FacilityCatalog.vue'), 'utf8');

/**
 * 移动端（360–430px）契约（task-64）：
 * - 应用列表 <sm 只留名称+操作；状态/实例折入名称副行（sm:hidden 摘要）；操作保留详情+More；
 * - 设施记录表 <sm 只留 domain+操作（其余列 hideBelow>=lg）；
 * - 编辑器/对话框 <sm 单列（字段栅格均 sm: 起）；编辑器 footer 两列按钮、Dialog footer 单列 full width；
 * - PageHeader actions 由 shell 负责换行（本页不压缩标题）。
 */
describe('applications mobile list', () => {
  it('hides status/instances/updatedAt columns and folds status into the name cell below sm', () => {
    expect(source).toMatch(/key: 'status'[\s\S]*?hideBelow: 'sm' as const/);
    expect(source).toMatch(/key: 'instances'[\s\S]*?hideBelow: 'lg' as const/);
    expect(source).toMatch(/key: 'updatedAt'[\s\S]*?hideBelow: 'wide' as const/);

    // <sm 名称副行里用 sm:hidden 保留状态/实例摘要，避免移动端丢信息。
    const nameBlock = source.slice(source.indexOf('<template #name="{ row }">'), source.indexOf('<template #status="{ row }">'));
    expect(nameBlock).toContain('sm:hidden');
    expect(nameBlock).toContain('<StatusBadge');
    expect(nameBlock).toContain("t('applicationsPage.nodeBucket.healthy'");
    // 名称在移动端收窄，避免名称+操作把表格撑出屏幕。
    expect(nameBlock).toContain('class="max-w-44 sm:max-w-72"');
  });

  it('keeps details + More as the only row actions and keeps every action in the More menu', () => {
    const start = source.indexOf('<template #actions="{ row }">');
    const actions = source.slice(start, source.indexOf('<template #pagination>', start));
    expect(actions).toContain("<Eye />{{ t('common.details') }}");
    expect(actions).toContain('class="hidden items-center gap-1 wide:flex"');
    const menu = actions.slice(actions.indexOf('<Dropdown '));
    for (const key of ["t('common.edit')", "t('applicationsPage.sync')", "t('common.delete')", "t('applicationsPage.logs')"]) {
      expect(menu).toContain(key);
    }
  });
});

describe('applications mobile facility table', () => {
  it('keeps only domain + actions columns visible below sm', () => {
    const block = source.slice(source.indexOf('const configRecordColumns'), source.indexOf('function configRecordSourceLabel'));
    const lines = block.split('\n').filter((line) => line.includes("key: '"));
    const visibleAtMobile = lines.filter((line) => !line.includes('hideBelow')).map((line) => line.trim());
    expect(visibleAtMobile).toHaveLength(2);
    expect(visibleAtMobile[0]).toContain("key: 'domain'");
    expect(visibleAtMobile[1]).toContain("key: 'actions'");
  });
});

describe('applications mobile editor and dialog footers', () => {
  it('stacks editor footer buttons into two full-width columns below sm', () => {
    expect(source.match(/flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between/g) ?? []).toHaveLength(2);
    expect(source.match(/grid grid-cols-2 gap-2 sm:flex sm:flex-wrap/g) ?? []).toHaveLength(2);
    const footerButtons = source.match(/class="w-full sm:w-auto"/g) ?? [];
    expect(footerButtons.length).toBeGreaterThanOrEqual(4);
  });

  it('makes the shared form dialog footer single-column full width below sm', () => {
    const footerStart = source.indexOf('<template #footer>\n      <div class="grid w-full grid-cols-1 gap-2 sm:flex');
    expect(footerStart).toBeGreaterThan(-1);
    const footer = source.slice(footerStart, source.indexOf('</template>', footerStart));
    expect(footer.match(/class="w-full sm:w-auto"/g)?.length ?? 0).toBeGreaterThanOrEqual(3);
  });

  it('uses no base two/three-column field grids (dialogs/editors are single column below sm)', () => {
    // 仅两个编辑器 footer 的按钮组使用 base grid-cols-2；字段栅格全部 sm: 起。
    const baseGrids = source.match(/(?<![\w:-])grid-cols-[23]\b/g) ?? [];
    expect(baseGrids).toHaveLength(2);
    expect(source.match(/grid grid-cols-2 gap-2 sm:flex/g) ?? []).toHaveLength(2);
  });
});

describe('applications mobile facility catalog', () => {
  it('stacks facility stat tiles into a single column below sm', () => {
    expect(catalogSource).toContain('grid grid-cols-1 gap-3 border-t border-divider pt-3 sm:grid-cols-3');
  });
});
