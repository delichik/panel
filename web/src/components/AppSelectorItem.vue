<script setup lang="ts">
const props = withDefaults(defineProps<{
  selected?: boolean;
  as?: 'button' | 'div';
}>(), {
  selected: false,
  as: 'button',
});

const emit = defineEmits<{
  select: [];
}>();

function selectFromKeyboard(event: KeyboardEvent) {
  if (props.as !== 'div' || (event.key !== 'Enter' && event.key !== ' ')) return;
  event.preventDefault();
  emit('select');
}
</script>

<template>
  <component
    :is="as"
    class="app-selector-item"
    :class="{ 'app-selector-item--selected': selected }"
    :type="as === 'button' ? 'button' : undefined"
    :role="as === 'div' ? 'option' : undefined"
    :tabindex="as === 'div' ? 0 : undefined"
    :aria-selected="as === 'div' ? selected : undefined"
    @click="emit('select')"
    @keydown="selectFromKeyboard"
  >
    <slot />
  </component>
</template>

<style scoped>
.app-selector-item {
  position: relative;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  width: 100%;
  min-width: 0;
  padding: 12px 12px 12px 14px;
  border: 1px solid color-mix(in srgb, var(--lp-border), transparent 46%);
  border-radius: 8px;
  background:
    linear-gradient(90deg, color-mix(in srgb, var(--lp-surface-container), transparent 64%), transparent 74%),
    color-mix(in srgb, var(--lp-surface), transparent 10%);
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: background-color 0.16s ease, border-color 0.16s ease, transform 0.16s ease, box-shadow 0.16s ease;
}

.app-selector-item::before {
  content: '';
  position: absolute;
  inset: 9px auto 9px 0;
  width: 3px;
  border-radius: 0 99px 99px 0;
  background: transparent;
  transition: background-color 0.16s ease;
}

.app-selector-item:hover {
  border-color: color-mix(in srgb, rgb(var(--v-theme-primary)), var(--lp-border) 82%);
  background:
    linear-gradient(90deg, rgba(var(--v-theme-primary), 0.035), transparent 76%),
    var(--lp-surface);
}

.app-selector-item:focus-visible {
  outline: 2px solid rgba(var(--v-theme-primary), 0.55);
  outline-offset: 1px;
}

.app-selector-item--selected {
  border-color: rgba(var(--v-theme-primary), 0.28);
  background:
    linear-gradient(90deg, rgba(var(--v-theme-primary), 0.095), rgba(var(--v-theme-primary), 0.026) 78%),
    var(--lp-surface);
  box-shadow: inset 0 0 0 1px rgba(var(--v-theme-primary), 0.035);
}

.app-selector-item--selected::before {
  background: rgb(var(--v-theme-primary));
}
</style>
