<script setup lang="ts">
import { computed, useId } from 'vue';
import { focusRing } from './states';
import { cn } from './cn';

/**
 * WinUI 3 RadioButton：`role=radio` + `aria-checked`，配合外部 `role=radiogroup`
 * 使用（方向键/roving tabindex 由分组容器负责；选中项在分组内 tabindex=0）。
 * 视觉：20px 圆形 + 选中 accent 填充 + 前景色圆点；命中区 ≥32px。
 */
const props = withDefaults(defineProps<{
  modelValue?: string | number | null;
  value: string | number;
  label: string;
  description?: string;
  disabled?: boolean;
  id?: string;
}>(), {
  modelValue: null,
  description: '',
  disabled: false,
});

const emit = defineEmits<{ 'update:modelValue': [value: string | number] }>();
const descriptionId = useId();

const checked = computed(() => props.modelValue === props.value);
const classes = computed(() => cn(
  'group flex min-h-control w-full items-start gap-2 rounded-control px-1 py-1 text-left transition-fill duration-fast ease-move',
  focusRing,
  props.disabled ? 'cursor-not-allowed text-muted-foreground' : 'cursor-pointer text-foreground',
));

function select() {
  if (props.disabled) return;
  if (!checked.value) emit('update:modelValue', props.value);
}
</script>

<template>
  <button
    :id="id"
    type="button"
    role="radio"
    :aria-checked="checked"
    :aria-describedby="description ? descriptionId : undefined"
    :tabindex="!disabled && checked ? 0 : -1"
    :disabled="disabled"
    :class="classes"
    @click="select"
  >
    <span
      class="mt-0.5 grid size-5 shrink-0 place-items-center rounded-full border transition-fill duration-fast ease-move group-active:duration-fade"
      :class="disabled
        ? 'border-stroke bg-muted'
        : checked
          ? 'border-primary bg-primary'
          : 'border-input bg-control group-hover:border-border-strong group-hover:bg-hover group-active:bg-pressed'"
      aria-hidden="true"
    >
      <span v-if="checked" class="size-2 rounded-full" :class="disabled ? 'bg-muted-foreground' : 'bg-primary-foreground'" />
    </span>
    <span class="min-w-0 flex-1">
      <span class="block text-body">{{ label }}</span>
      <span v-if="description" :id="descriptionId" class="mt-0.5 block text-caption text-muted-foreground">{{ description }}</span>
    </span>
  </button>
</template>
