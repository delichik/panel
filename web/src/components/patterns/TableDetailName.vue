<script setup lang="ts">
/**
 * Name cell control that opens a covered detail without making the whole table
 * row clickable (UI-SHELL-061).
 */
defineProps<{
  name: string;
  /** Localized accessible name, e.g. "Open details for alpha". */
  label: string;
}>();

const emit = defineEmits<{ click: [] }>();
</script>

<template>
  <button
    type="button"
    class="max-w-full truncate rounded-control text-left text-body-strong text-foreground transition-colors duration-fast hover:text-accent-text focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    :aria-label="label"
    :title="name"
    @click.stop="emit('click')"
  >
    {{ name }}
  </button>
</template>
