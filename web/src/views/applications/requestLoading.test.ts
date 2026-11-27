import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const viewSource = readFileSync(resolve(__dirname, 'index.vue'), 'utf8');

/**
 * 请求/模式切换收口契约（修复批次 4）：
 * - 进入页面/切换 mode 走 ensureModeData()，不再无条件 loadServers + loadApplications；
 * - 返回列表命中已加载查询键时不重复请求（显式刷新仍走 load()）；
 * - 行图片详情不再对每行立即 GET：延迟队列 + 缓存 + attempted 去重 + 并发去重。
 */
describe('applications request loading', () => {
  it('loads only the data the current mode needs on route changes', () => {
    // route.path watcher / onMounted 走按需加载，而不是全量 load()。
    expect(viewSource).toContain('async function ensureModeData()');
    expect(viewSource).toContain('await ensureModeData();');
    expect(viewSource).not.toContain('await load();\n  if (isAppEditor.value)');

    // 列表缓存键 + 命中跳过；服务器按需且并发去重。
    expect(viewSource).toContain('applicationsLoadedKey === applicationsListQueryKey()');
    expect(viewSource).toContain('async function ensureServers()');
    expect(viewSource).toContain('async function ensureApplicationsList()');
    expect(viewSource).toContain('applicationsLoadPromise');
  });

  it('does not fire row image GETs for every row immediately', () => {
    // 旧的逐行立即请求已移除。
    expect(viewSource).not.toContain('Object.keys(missing).forEach((id) => { void loadRowImageDetail(id); });');
    // 延迟 + 串行队列，且同 id 只尝试一次。
    expect(viewSource).toContain('enqueueRowImagePrefetch(Object.keys(missing))');
    expect(viewSource).toContain('setTimeout(');
    expect(viewSource).toContain('async function drainRowImageQueue()');
    expect(viewSource).toContain('rowImageAttempted.has(applicationId)');
    expect(viewSource).toContain('rowImageRefs.value[applicationId]');
    // 失败/刷新不会回退成请求风暴，也不会丢已解析缓存。
    expect(viewSource).toContain('const rowImageAttempted = new Set<string>();');
  });
});
