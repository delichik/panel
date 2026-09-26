<script setup lang="ts">
import { computed } from 'vue';
import { ArrowLeft } from '@lucide/vue';
import Button from '@/components/ui/Button.vue';

/*
 * 一级对象选择工作台的统一几何：默认单列，xl 起左栏 360px + 自适应右栏。
 * 高度契约（h-full + min-h-0）由模板自身承担，页面不再需要 min-h-[Npx] 兜底。
 * 注意：模板必须是单一元素根，否则页面传入的 class 无法透传（组件会退化为 fragment）。
 */
const props = defineProps<{
  /**
   * Identity of the currently selected detail object. When it changes the new
   * detail content is animated in; the outgoing content is removed immediately
   * so a list click still feels instant.
   */
  detailKey?: string | number;
  /**
   * Whether a detail object is currently selected. Only meaningful for pages
   * that opt into narrow-screen single view via `backLabel`.
   */
  hasDetail?: boolean;
  /**
   * Localized label for the narrow-screen back action. Passing it opts the
   * layout into single-view switching below xl (list ⇄ detail, one pane at a
   * time); pages that omit it always render both panes (they own their own
   * switching, such as activity).
   */
  backLabel?: string;
}>();

const emit = defineEmits<{ back: [] }>();

const detailTransitionKey = computed(() => String(props.detailKey ?? ''));
// 说明：用字符串 prop 而不是布尔 prop 作为“接入单视图”的信号，因为 Vue 会把
// 未传入的 Boolean prop 强制转换为 false，无法与“显式传 false”区分。
const singleView = computed(() => Boolean(props.backLabel));
const listVisible = computed(() => !singleView.value || props.hasDetail !== true);
const detailVisible = computed(() => !singleView.value || props.hasDetail === true);
</script>

<template>
  <div class="grid h-full min-h-0 min-w-0 grid-cols-1 gap-4 overflow-x-hidden xl:grid-cols-[360px_minmax(0,1fr)]">
    <div class="grid min-h-0 min-w-0" :class="listVisible ? undefined : 'max-xl:hidden'">
      <slot name="master" />
    </div>
    <div class="grid min-h-0 min-w-0" :class="detailVisible ? undefined : 'max-xl:hidden'">
      <div class="grid min-h-0 min-w-0 max-xl:grid-rows-[auto_minmax(0,1fr)] xl:grid-rows-[minmax(0,1fr)]">
        <div v-if="!listVisible && backLabel" class="mb-3 xl:hidden">
          <Button size="sm" variant="secondary" @click="emit('back')">
            <ArrowLeft />{{ backLabel }}
          </Button>
        </div>
        <Transition name="detail-swap" mode="out-in">
          <div :key="detailTransitionKey" class="grid min-h-0 min-w-0">
            <slot name="detail" />
          </div>
        </Transition>
      </div>
    </div>
  </div>
</template>
