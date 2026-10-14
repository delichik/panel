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
    class="motion-control max-w-full truncate rounded-md text-left text-sm font-semibold text-foreground hover:text-brand focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:ring-offset-2 focus-visible:ring-offset-card"
    :aria-label="label"
    :title="name"
    @click.stop="emit('click')"
  >
    {{ name }}
  </button>
</template>
