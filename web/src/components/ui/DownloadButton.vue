<script setup lang="ts">
import { Download } from '@lucide/vue';
import Button from './Button.vue';

withDefaults(defineProps<{
  loading?: boolean;
  disabled?: boolean;
  label: string;
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
  size?: 'sm' | 'md' | 'lg';
}>(), {
  variant: 'secondary',
  size: 'md',
});

defineEmits<{ click: [] }>();
</script>

<template>
  <Button :variant="variant" :size="size" :loading="loading" :disabled="disabled" @click="$emit('click')">
    <slot name="icon"><Download /></slot>
    <slot>{{ label }}</slot>
  </Button>
</template>
