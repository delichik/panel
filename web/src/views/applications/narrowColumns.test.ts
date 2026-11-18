// @vitest-environment jsdom
import { mount } from '@vue/test-utils';
import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import Table from '@/components/ui/Table.vue';

const source = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');

function sourceBlock(startMarker: string, endMarker: string) {
  const start = source.indexOf(startMarker);
  const end = source.indexOf(endMarker, start + startMarker.length);
  expect(start, `start marker: ${startMarker}`).toBeGreaterThan(-1);
  expect(end, `end marker: ${endMarker}`).toBeGreaterThan(start);
  return source.slice(start, end);
}

function columnLine(block: string, key: string) {
  const line = block.split('\n').find((entry) => entry.includes(`key: '${key}'`));
  expect(line, `column ${key}`).toBeTruthy();
  return line!;
}

/** 返回某一列 actions 插槽内 `<Dropdown ...>` 菜单片段。 */
function dropdownMenu(block: string) {
  const start = block.indexOf('<Dropdown ');
  expect(start, 'Dropdown menu').toBeGreaterThan(-1);
  return block.slice(start);
}

/**
 * 窄屏列与操作折叠契约（task-58）：
 * - Table 列 `hideBelow` 生成 `max-<bp>:hidden`；应用列表 instances<lg、updatedAt<wide 隐藏；
 * - <wide 行内编辑/同步收进 More，仅保留详情 + 更多；More 必须包含被隐藏的主操作；
 * - 设施记录表同样按优先级隐藏次级列并折叠操作。
 */
describe('applications narrow columns', () => {
  it('hides secondary application columns by priority', () => {
    const block = sourceBlock('const applicationColumns', 'const logsApplication');
    expect(columnLine(block, 'instances')).toContain("hideBelow: 'lg'");
    expect(columnLine(block, 'updatedAt')).toContain("hideBelow: 'wide'");
    // 名称/状态/操作始终保留。
    expect(columnLine(block, 'name')).not.toContain('hideBelow');
    expect(columnLine(block, 'status')).not.toContain('hideBelow');
    expect(columnLine(block, 'actions')).not.toContain('hideBelow');
  });

  it('hides secondary facility record columns by priority', () => {
    const block = sourceBlock('const configRecordColumns', 'function configRecordSourceLabel');
    expect(columnLine(block, 'source')).toContain("hideBelow: 'lg'");
    expect(columnLine(block, 'version')).toContain("hideBelow: 'lg'");
    expect(columnLine(block, 'dns')).toContain("hideBelow: 'wide'");
    expect(columnLine(block, 'updatedAt')).toContain("hideBelow: 'wide'");
    expect(columnLine(block, 'domain')).not.toContain('hideBelow');
    expect(columnLine(block, 'actions')).not.toContain('hideBelow');
  });

  it('renders the hideBelow contract as max-*:hidden classes on the shared Table', () => {
    const wrapper = mount(Table, {
      props: {
        columns: [
          { key: 'name', label: 'Name' },
          { key: 'instances', label: 'Instances', hideBelow: 'lg' },
          { key: 'updatedAt', label: 'Updated', hideBelow: 'wide' },
        ],
        rows: [{ name: 'api', instances: 1, updatedAt: 'now' }],
      },
    });
    expect(wrapper.get('thead th:nth-child(2)').classes()).toContain('max-lg:hidden');
    expect(wrapper.get('thead th:nth-child(3)').classes()).toContain('max-wide:hidden');
    expect(wrapper.get('tbody td:nth-child(2)').classes()).toContain('max-lg:hidden');
    expect(wrapper.get('tbody td:nth-child(3)').classes()).toContain('max-wide:hidden');
  });
});

describe('applications action folding below wide', () => {
  it('keeps details + more visible and folds inline edit/sync into the menu', () => {
    const block = sourceBlock('<template #actions="{ row }">', '<template #empty>');
    // 详情按钮不被 wide 隐藏，且位于折叠组之前。
    const detailsAt = block.indexOf("<Eye />{{ t('common.details') }}");
    const collapsedAt = block.indexOf('class="hidden items-center gap-1 wide:flex"');
    expect(detailsAt).toBeGreaterThan(-1);
    expect(collapsedAt).toBeGreaterThan(detailsAt);
    // 行内组包含编辑 + 同步。
    const collapsed = block.slice(collapsedAt, block.indexOf('</div>', collapsedAt));
    expect(collapsed).toContain("t('common.edit')");
    expect(collapsed).toContain("t('applicationsPage.sync')");
    // More 菜单始终包含被折叠的主操作（编辑/同步）。
    const menu = dropdownMenu(block);
    expect(menu).toContain("router.push(`/applications/apps/${encodeURIComponent(row.app.id)}/edit`)");
    expect(menu).toContain("<Wrench />{{ t('common.edit') }}");
    expect(menu).toContain('@click="deployFor(row.app)"');
    expect(menu).toContain("t('applicationsPage.sync')");
  });

  it('folds facility record actions into a More menu below wide', () => {
    const facilityBlock = sourceBlock("row.kind === 'facility_domain'", 'configRecordEmpty');
    // 行内组/按钮在 <wide 隐藏。
    expect(facilityBlock).toContain('class="hidden items-center gap-1 wide:flex"');
    expect(facilityBlock).toContain('class="hidden wide:inline-flex"');
    // More 菜单包含全部被折叠操作（编辑/应用/删除/打开应用）。
    const menu = dropdownMenu(facilityBlock);
    expect(menu).toContain("t('common.edit')");
    expect(menu).toContain("t('common.apply')");
    expect(menu).toContain("t('common.delete')");
    expect(menu).toContain("t('applicationsPage.configRecordOpenApplication')");
  });
});
