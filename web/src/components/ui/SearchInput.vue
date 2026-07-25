<script setup lang="ts">
import { Search, X } from '@lucide/vue';
import Input from './Input.vue';
import IconButton from './IconButton.vue';

withDefaults(defineProps<{
  modelValue?: string;
  placeholder?: string;
  clearable?: boolean;
  disabled?: boolean;
  label?: string;
  clearLabel?: string;
}>(), {
  clearable: false,
});

defineEmits<{
  'update:modelValue': [value: string];
  clear: [];
}>();
</script>

<template>
  <label class="relative block min-w-0">
    <span v-if="label" class="sr-only">{{ label }}</span>
    <Search class="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
    <Input
      :model-value="modelValue"
      :placeholder="placeholder"
      :disabled="disabled"
      class="pl-9"
      :class="clearable && modelValue ? 'pr-9' : undefined"
      type="search"
      @update:model-value="$emit('update:modelValue', $event)"
    />
    <IconButton
      v-if="clearable && modelValue"
      class="absolute right-0.5 top-0.5"
      size="sm"
      :disabled="disabled"
      :label="clearLabel ?? label ?? placeholder ?? ''"
      @click="$emit('update:modelValue', ''); $emit('clear')"
    >
      <X />
    </IconButton>
  </label>
</template>
