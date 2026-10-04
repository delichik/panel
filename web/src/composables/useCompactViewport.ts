import { onBeforeUnmount, ref } from 'vue';

/**
 * 与 MasterDetailLayout 单视图保持同一断点：xl（1280px）以下视为紧凑视口。
 */
const COMPACT_QUERY = '(max-width: 1279.98px)';

/**
 * 紧凑视口检测。主从工作台页面用它决定“是否自动选中第一条对象”：
 * 窄屏/平板上应停在列表或服务器选择器，由用户主动进入详情（详情带返回操作），
 * 宽屏保持“默认选中第一条 + 双栏并排”的既有行为。
 */
export function useCompactViewport() {
  const compact = ref(false);
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return compact;

  const query = window.matchMedia(COMPACT_QUERY);
  compact.value = query.matches;
  const onChange = (event: MediaQueryListEvent) => {
    compact.value = event.matches;
  };
  query.addEventListener('change', onChange);
  onBeforeUnmount(() => query.removeEventListener('change', onChange));
  return compact;
}
