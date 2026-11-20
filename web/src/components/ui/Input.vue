<script setup lang="ts">
import { computed, useAttrs } from 'vue';
import { fieldDisabled, fieldFocus } from './states';
import { cn } from './cn';

defineOptions({ inheritAttrs: false });
defineProps<{ modelValue?: string | number; invalid?: boolean }>();
defineEmits<{ 'update:modelValue': [value: string] }>();

const attrs = useAttrs();
// WinUI TextBox：Body 14、32px、4px 圆角、填充底 + 1px 描边；聚焦保留 1px 描边并出现 2px 底部强调线。
const classes = computed(() => cn(
  'h-control w-full min-w-field surface-control px-3 text-body text-foreground transition-state duration-fast ease-move placeholder:text-muted-foreground hover:border-border-strong',
  fieldFocus,
  fieldDisabled,
  attrs.class as string,
));
</script>

<template>
  <input
    v-bind="{ ...attrs, class: undefined }"
    :value="modelValue"
    :aria-invalid="invalid || undefined"
    :class="[classes, invalid ? 'border-danger-border focus:border-danger-border focus:focus-danger-underline' : undefined]"
    @input="$emit('update:modelValue', ($event.target as HTMLInputElement).value)"
  />
</template>
