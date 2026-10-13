<script setup lang="ts">
import { computed } from 'vue';

/*
 * 表格 + 覆盖式详情的统一几何：列表与详情叠放在同一网格单元，详情覆盖整个内容区。
 * 列表保持挂载（inert + aria-hidden），关闭详情后滚动位置、筛选与分页状态不丢失；
 * 详情只播放一次进场（detail-swap-*，仅进场），与主从详情切换的动效约定一致。
 * 注意：模板必须是单一元素根，否则页面传入的 class 无法透传（组件会退化为 fragment）。
 */
const props = withDefaults(defineProps<{
  /** Whether the covered detail view is currently open. */
  hasDetail?: boolean;
}>(), { hasDetail: false });

const detailVisible = computed(() => props.hasDetail === true);
</script>

<template>
  <div class="grid h-full min-h-0 min-w-0">
    <div
      class="col-start-1 row-start-1 grid min-h-0 min-w-0"
      :inert="detailVisible || undefined"
      :aria-hidden="detailVisible ? 'true' : undefined"
    >
      <slot name="list" />
    </div>
    <Transition name="detail-swap">
      <div v-if="detailVisible" class="col-start-1 row-start-1 grid min-h-0 min-w-0 bg-background">
        <slot name="detail" />
      </div>
    </Transition>
  </div>
</template>
