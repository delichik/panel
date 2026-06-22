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
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  width: 100%;
  min-width: 0;
  padding: 11px 12px;
  border: 1px solid transparent;
  border-radius: 8px;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: background-color 0.16s ease, border-color 0.16s ease;
}

.app-selector-item:hover {
  background: rgba(var(--v-theme-on-surface), 0.025);
}

.app-selector-item:focus-visible {
  outline: 2px solid rgba(var(--v-theme-primary), 0.55);
  outline-offset: 1px;
}

.app-selector-item--selected {
  border-color: rgba(var(--v-theme-primary), 0.26);
  background: rgba(var(--v-theme-primary), 0.06);
}
</style>
