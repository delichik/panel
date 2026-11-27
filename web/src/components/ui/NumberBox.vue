<script setup lang="ts">
import { ChevronDown } from '@/icons';
import { computed, ref, useAttrs, watch } from 'vue';
import { cn } from './cn';

defineOptions({ inheritAttrs: false });

/**
 * WinUI 3 NumberBox：32px 控件、spinbutton 语义、step/clamp 与键盘 Up/Down/Home/End。
 * 右侧两个 32px 按钮复用 ChevronDown（上箭头旋转 180°）表示增减，避免新增图标。
 * 按钮对辅助技术隐藏并移出 Tab 序列；键盘用户通过输入框的方向键操作。
 */
const props = withDefaults(defineProps<{
  modelValue: number | null;
  min?: number;
  max?: number;
  step?: number;
  disabled?: boolean;
  invalid?: boolean;
  id?: string;
  ariaDescribedby?: string;
}>(), {
  min: undefined,
  max: undefined,
  step: 1,
  disabled: false,
  invalid: false,
});

const emit = defineEmits<{ 'update:modelValue': [value: number | null] }>();
const attrs = useAttrs();
const input = ref<HTMLInputElement | null>(null);
function format(value: number | null | undefined): string {
  return value === null || value === undefined ? '' : String(value);
}

const text = ref(format(props.modelValue));

function clamp(value: number): number {
  let next = value;
  if (props.min !== undefined && next < props.min) next = props.min;
  if (props.max !== undefined && next > props.max) next = props.max;
  return next;
}

function emitNumber(value: number | null) {
  const next = value === null ? null : clamp(value);
  if (next !== props.modelValue) emit('update:modelValue', next);
}

function commit() {
  const raw = text.value.trim();
  if (raw === '') {
    text.value = '';
    emitNumber(null);
    return;
  }
  const parsed = Number(raw);
  if (!Number.isFinite(parsed)) {
    text.value = format(props.modelValue);
    return;
  }
  const next = clamp(parsed);
  text.value = format(next);
  emitNumber(next);
}

function onInput(event: Event) {
  text.value = (event.target as HTMLInputElement).value;
}

function bump(delta: number) {
  if (props.disabled) return;
  const base = props.modelValue ?? props.min ?? 0;
  const next = clamp(base + delta * props.step);
  text.value = format(next);
  emitNumber(next);
  input.value?.focus();
}

function onKeydown(event: KeyboardEvent) {
  if (props.disabled) return;
  if (event.key === 'ArrowUp') {
    event.preventDefault();
    bump(1);
    return;
  }
  if (event.key === 'ArrowDown') {
    event.preventDefault();
    bump(-1);
    return;
  }
  if (event.key === 'Home' && props.min !== undefined) {
    event.preventDefault();
    text.value = format(props.min);
    emitNumber(props.min);
    return;
  }
  if (event.key === 'End' && props.max !== undefined) {
    event.preventDefault();
    text.value = format(props.max);
    emitNumber(props.max);
  }
}

watch(() => props.modelValue, (value) => {
  const parsed = text.value.trim() === '' ? null : Number(text.value);
  if (parsed !== value) text.value = format(value);
});

const wrapperClasses = computed(() => cn(
  'flex h-control w-full min-w-field items-stretch overflow-hidden rounded-control border transition-state duration-fast ease-move',
  'surface-control focus-within:ring-2 focus-within:ring-ring',
  !props.disabled && 'hover:border-border-strong',
  props.invalid && 'border-danger-border focus-within:ring-danger',
  props.disabled && 'border-stroke bg-muted',
  attrs.class as string,
));

const inputAttrs = computed(() => {
  const { class: _class, ...rest } = attrs;
  return rest;
});
</script>

<template>
  <div :class="wrapperClasses">
    <input
      :id="id"
      ref="input"
      v-bind="inputAttrs"
      :value="text"
      type="text"
      inputmode="decimal"
      role="spinbutton"
      :aria-valuemin="min"
      :aria-valuemax="max"
      :aria-valuenow="modelValue ?? undefined"
      :aria-invalid="invalid || undefined"
      :aria-describedby="ariaDescribedby"
      :disabled="disabled"
      class="h-control w-full min-w-0 bg-transparent px-3 text-body outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed"
      :class="disabled ? 'text-muted-foreground' : 'text-foreground'"
      @input="onInput"
      @change="commit"
      @blur="commit"
      @keydown="onKeydown"
    />
    <span class="flex shrink-0 items-stretch" aria-hidden="true">
      <button
        type="button"
        tabindex="-1"
        :disabled="disabled"
        class="grid w-control place-items-center border-l border-stroke text-secondary-foreground transition-colors hover:bg-hover active:bg-pressed disabled:cursor-not-allowed"
        @mousedown.prevent
        @click="bump(-1)"
      >
        <ChevronDown class="size-4" />
      </button>
      <button
        type="button"
        tabindex="-1"
        :disabled="disabled"
        class="grid w-control place-items-center border-l border-stroke text-secondary-foreground transition-colors hover:bg-hover active:bg-pressed disabled:cursor-not-allowed"
        @mousedown.prevent
        @click="bump(1)"
      >
        <ChevronDown class="size-4 rotate-180" />
      </button>
    </span>
  </div>
</template>
