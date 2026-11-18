<script setup lang="ts">
import { computed, ref, watch } from 'vue';

/*
 * 表格 + 覆盖式详情的统一几何：列表与详情叠放在同一网格单元，详情覆盖整个内容区。
 * 列表保持挂载（inert + aria-hidden，且视觉上隐藏），关闭详情后滚动位置、筛选与分页状态不丢失；
 * 详情只播放一次进场（detail-swap-*，仅进场），与主从详情切换的动效约定一致。
 *
 * 层叠：模板根 `isolate` 建立独立层叠上下文，列表格 `relative z-0` 把表格 sticky 表头
 * （z-10）、Tooltip（z-40）等带 z-index 的元素限制在列表层；详情格 `relative z-10`
 * 覆盖其上层。否则静态定位的详情背景会被列表内带 z-index 的元素刺穿（表格看似“透出来”），
 * 且列表内部 z-index 也不得与外壳顶栏（窄屏 z-40）争抢层级。
 *
 * 表面：WinUI 内容层是半透明的（透出 Mica），详情格不能再画不透明 bg-background，
 * 否则 Mica 开启时会出现一块实色补丁；改为隐藏列表格并让详情直接坐在外壳内容层上，
 * 视觉与滚动契约都与原来一致。
 *
 * 注意：模板必须是单一元素根，否则页面传入的 class 无法透传（组件会退化为 fragment）。
 */
const props = withDefaults(defineProps<{
  /** Whether the covered detail view is currently open. */
  hasDetail?: boolean;
}>(), { hasDetail: false });

const detailVisible = computed(() => props.hasDetail === true);
// Keep the list hidden through the detail's leave transition; otherwise the
// transparent detail surface would expose the list while its text is still
// fading out.
const detailLeaving = ref(false);
watch(detailVisible, (visible, previous) => {
  if (previous && !visible) detailLeaving.value = true;
});
const listHidden = computed(() => detailVisible.value || detailLeaving.value);
</script>

<template>
  <div class="isolate grid h-full min-h-0 min-w-0">
    <div
      class="relative z-0 col-start-1 row-start-1 grid min-h-0 min-w-0"
      :class="listHidden ? 'invisible' : undefined"
      :inert="listHidden || undefined"
      :aria-hidden="listHidden ? 'true' : undefined"
    >
      <slot name="list" />
    </div>
    <Transition name="detail-swap" @after-leave="detailLeaving = false">
      <div v-if="detailVisible" class="relative z-10 col-start-1 row-start-1 grid min-h-0 min-w-0">
        <slot name="detail" />
      </div>
    </Transition>
  </div>
</template>
