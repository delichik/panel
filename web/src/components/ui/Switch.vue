<script setup lang="ts">
defineProps<{ modelValue: boolean; disabled?: boolean; label: string }>();
defineEmits<{ 'update:modelValue': [value: boolean] }>();
</script>

<template>
  <button
    type="button"
    role="switch"
    :aria-checked="modelValue"
    :aria-label="label"
    :disabled="disabled"
    class="motion-switch relative h-6 w-11 rounded-full border border-border bg-muted transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:ring-offset-2 focus-visible:ring-offset-card disabled:pointer-events-none disabled:opacity-45 aria-checked:bg-primary"
    @click="$emit('update:modelValue', !modelValue)"
  >
    <span class="absolute left-0.5 top-0.5 size-5 rounded-full bg-card shadow-sm transition-transform duration-200 ease-out" :class="modelValue ? 'translate-x-5' : 'translate-x-0'" />
  </button>
</template>
