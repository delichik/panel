<script setup lang="ts">
import { computed, useAttrs } from 'vue';
import { cn } from './cn';

defineOptions({ inheritAttrs: false });
defineProps<{ modelValue?: string | number; invalid?: boolean }>();
defineEmits<{ 'update:modelValue': [value: string] }>();

const attrs = useAttrs();
const classes = computed(() => cn(
  'motion-field h-9 w-full rounded-xl border bg-card px-3 text-sm text-foreground transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:ring-offset-2 focus-visible:ring-offset-card disabled:cursor-not-allowed disabled:opacity-45',
  attrs.class as string,
));
</script>

<template>
  <input
    v-bind="{ ...attrs, class: undefined }"
    :value="modelValue"
    :aria-invalid="invalid || undefined"
    :class="[classes, invalid ? 'border-danger-border' : 'border-input focus-visible:border-ring']"
    @input="$emit('update:modelValue', ($event.target as HTMLInputElement).value)"
  />
</template>
