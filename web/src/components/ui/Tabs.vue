<script setup lang="ts">
defineProps<{
  modelValue: string;
  tabs: Array<{ label: string; value: string; disabled?: boolean }>;
}>();
defineEmits<{ 'update:modelValue': [value: string] }>();
</script>

<template>
  <div class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)]">
    <div class="inline-flex w-fit rounded-xl border border-border bg-muted p-1" role="tablist">
      <button
        v-for="tab in tabs"
        :key="tab.value"
        type="button"
        role="tab"
        :aria-selected="modelValue === tab.value"
        :disabled="tab.disabled"
        class="motion-tab h-8 rounded-lg px-3 text-sm font-medium text-muted-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 disabled:pointer-events-none disabled:opacity-45 aria-selected:bg-background aria-selected:text-foreground aria-selected:shadow-sm"
        @click="$emit('update:modelValue', tab.value)"
      >
        {{ tab.label }}
      </button>
    </div>
    <div class="motion-enter min-h-0 pt-4">
      <slot />
    </div>
  </div>
</template>
