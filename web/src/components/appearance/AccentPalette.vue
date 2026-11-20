<script setup lang="ts">
import { Check } from '@/icons';
import { useI18n } from '@/i18n';
import { focusWithoutScroll } from '@/utils/focus';
import type { AccentId } from '@/types/appearance';

const props = withDefaults(defineProps<{
  modelValue: AccentId;
  label: string;
  /** `accentPresets` from `useAppearance()`: contract seed per accent id. */
  presets: readonly { id: AccentId; seed: string }[];
  disabled?: boolean;
}>(), {
  disabled: false,
});

const emit = defineEmits<{
  'update:modelValue': [value: AccentId];
}>();

const { t } = useI18n();

function select(value: AccentId) {
  if (props.disabled || value === props.modelValue) return;
  emit('update:modelValue', value);
}

/** 单选组键盘模型：单 Tab 停靠 + 方向键/Home/End 在色板内移动。 */
function nextIndex(event: KeyboardEvent, index: number): number {
  const total = props.presets.length;
  if (!total) return -1;
  if (event.key === 'ArrowRight' || event.key === 'ArrowDown') return (index + 1) % total;
  if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') return (index - 1 + total) % total;
  if (event.key === 'Home') return 0;
  if (event.key === 'End') return total - 1;
  return -1;
}

function onKeydown(event: KeyboardEvent, index: number) {
  if (props.disabled) return;
  const next = nextIndex(event, index);
  if (next < 0) return;
  event.preventDefault();
  select(props.presets[next]!.id);
  const radios = (event.currentTarget as HTMLElement | null)?.parentElement?.querySelectorAll<HTMLButtonElement>('[role="radio"]');
  focusWithoutScroll(radios?.[next]);
}
</script>

<template>
  <div role="radiogroup" :aria-label="label" class="flex flex-wrap gap-2">
    <button
      v-for="(preset, index) in presets"
      :key="preset.id"
      type="button"
      role="radio"
      :aria-checked="modelValue === preset.id"
      :aria-label="t(`appearance.accent.${preset.id}`)"
      :tabindex="modelValue === preset.id ? 0 : -1"
      :disabled="disabled"
      class="motion-control grid size-8 place-items-center rounded-pill border border-stroke focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-45"
      :class="modelValue === preset.id ? 'ring-2 ring-ring' : 'hover:border-foreground'"
      :style="{ backgroundColor: preset.seed }"
      @click="select(preset.id)"
      @keydown="onKeydown($event, index)"
    >
      <Check v-if="modelValue === preset.id" class="size-4 text-primary-foreground" aria-hidden="true" />
    </button>
  </div>
</template>
