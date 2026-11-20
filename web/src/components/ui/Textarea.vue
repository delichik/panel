<script setup lang="ts">
import { computed, useAttrs } from 'vue';
import { fieldDisabled, fieldFocus } from './states';
import { cn } from './cn';

defineOptions({ inheritAttrs: false });
defineProps<{ modelValue?: string; invalid?: boolean }>();
defineEmits<{ 'update:modelValue': [value: string] }>();

const attrs = useAttrs();
const classes = computed(() => cn(
  'min-h-24 w-full min-w-field resize-y surface-control px-3 py-2 text-body text-foreground transition-state duration-fast ease-move placeholder:text-muted-foreground hover:border-border-strong',
  fieldFocus,
  fieldDisabled,
  attrs.class as string,
));
</script>

<template>
  <textarea
    v-bind="{ ...attrs, class: undefined }"
    :value="modelValue"
    :aria-invalid="invalid || undefined"
    :class="[classes, invalid ? 'border-danger-border focus:border-danger-border focus:focus-danger-underline' : undefined]"
    @input="$emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)"
  />
</template>
