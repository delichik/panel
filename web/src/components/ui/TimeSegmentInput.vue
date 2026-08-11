<script setup lang="ts">
import { computed } from 'vue';

const props = withDefaults(defineProps<{ modelValue: number; min?: number; max?: number; label?: string }>(), {
  min: 0,
  max: 59,
  label: '',
});

const emit = defineEmits<{ 'update:modelValue': [value: number] }>();

const display = computed(() => String(props.modelValue).padStart(2, '0'));

function clamp(value: number): number {
  return Math.min(props.max, Math.max(props.min, value));
}

function onInput(event: Event) {
  const raw = (event.target as HTMLInputElement).value.replace(/\D/g, '').slice(0, 2);
  emit('update:modelValue', clamp(raw === '' ? props.min : Number(raw)));
}

function onBlur(event: Event) {
  (event.target as HTMLInputElement).value = display.value;
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'ArrowUp') {
    event.preventDefault();
    emit('update:modelValue', clamp(props.modelValue + 1));
  } else if (event.key === 'ArrowDown') {
    event.preventDefault();
    emit('update:modelValue', clamp(props.modelValue - 1));
  }
}

function onWheel(event: WheelEvent) {
  // Only adjust the value while the field is focused; otherwise let the
  // surrounding page scroll normally.
  if (document.activeElement !== event.target) return;
  event.preventDefault();
  emit('update:modelValue', clamp(props.modelValue + (event.deltaY < 0 ? 1 : -1)));
}
</script>

<template>
  <input
    type="text"
    inputmode="numeric"
    :value="display"
    :aria-label="label"
    class="motion-field h-7 w-10 rounded-lg border border-input bg-background text-center text-xs tabular-nums text-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40"
    @input="onInput"
    @blur="onBlur"
    @keydown="onKeydown"
    @wheel="onWheel"
    @focus="($event.target as HTMLInputElement).select()"
  />
</template>