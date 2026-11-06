<script setup lang="ts">
import { UploadCloud } from '@lucide/vue';
import { ref } from 'vue';
import Button from './Button.vue';

withDefaults(defineProps<{
  accept?: string;
  multiple?: boolean;
  loading?: boolean;
  disabled?: boolean;
  label: string;
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
  size?: 'sm' | 'md' | 'lg';
}>(), {
  variant: 'secondary',
  size: 'md',
});

const emit = defineEmits<{
  change: [value: File | File[]];
}>();

const input = ref<HTMLInputElement | null>(null);

function openPicker() {
  input.value?.click();
}

function onChange(event: Event) {
  const target = event.target as HTMLInputElement;
  const files = Array.from(target.files ?? []);
  if (files.length === 0) return;
  emit('change', target.multiple ? files : files[0]);
  target.value = '';
}
</script>

<template>
  <Button :variant="variant" :size="size" :loading="loading" :disabled="disabled" @click="openPicker">
    <slot name="icon"><UploadCloud /></slot>
    <slot>{{ label }}</slot>
  </Button>
  <input ref="input" class="hidden" type="file" :accept="accept" :multiple="multiple" :disabled="disabled || loading" @change="onChange" />
</template>
