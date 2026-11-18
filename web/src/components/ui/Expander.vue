<script setup lang="ts">
import { ChevronDown } from '@/icons';
import { computed, ref, useId, watch } from 'vue';
import { focusRing } from './states';
import { cn } from './cn';

/**
 * WinUI 3 Expander：header 是 button（aria-expanded/aria-controls），ChevronDown
 * 旋转指示展开；内容用 v-show 保持 DOM（默认关闭，可传 open 初始展开）。
 */
const props = withDefaults(defineProps<{
  title: string;
  description?: string;
  open?: boolean;
  disabled?: boolean;
}>(), {
  description: '',
  open: false,
  disabled: false,
});

const emit = defineEmits<{ 'update:open': [value: boolean] }>();
const headerId = useId();
const panelId = useId();
const expanded = ref(props.open);

watch(() => props.open, (value) => {
  expanded.value = value;
});

const headerClasses = computed(() => cn(
  'group flex min-h-control w-full items-center gap-2 rounded-control px-2 py-1 text-left transition-fill duration-fast ease-move',
  focusRing,
  'hover:bg-hover',
  props.disabled ? 'cursor-not-allowed text-muted-foreground opacity-100' : 'text-foreground',
));

function toggle() {
  if (props.disabled) return;
  expanded.value = !expanded.value;
  emit('update:open', expanded.value);
}
</script>

<template>
  <div class="min-w-0">
    <button
      :id="headerId"
      type="button"
      :aria-expanded="expanded ? 'true' : 'false'"
      :aria-controls="panelId"
      :disabled="disabled"
      :class="headerClasses"
      @click="toggle"
    >
      <ChevronDown
        class="size-4 shrink-0 text-secondary-foreground transition-transform duration-fast ease-enter"
        :class="expanded ? 'rotate-180' : undefined"
        aria-hidden="true"
      />
      <span class="min-w-0 flex-1">
        <span class="block text-body-strong text-foreground">{{ title }}</span>
        <span v-if="description" class="mt-0.5 block text-caption text-muted-foreground">{{ description }}</span>
      </span>
      <span v-if="$slots.actions" class="flex shrink-0 items-center gap-2">
        <slot name="actions" />
      </span>
    </button>
    <div v-show="expanded" :id="panelId" role="region" :aria-labelledby="headerId" class="pt-2">
      <slot />
    </div>
  </div>
</template>
