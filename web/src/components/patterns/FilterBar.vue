<script setup lang="ts">
import SearchInput from '../ui/SearchInput.vue';
import Select from '../ui/Select.vue';

withDefaults(defineProps<{
  search?: string;
  searchPlaceholder?: string;
  searchLabel?: string;
  status?: string;
  statusOptions?: Array<{ label: string; value: string; disabled?: boolean }>;
  statusPlaceholder?: string;
  disabled?: boolean;
}>(), {
  search: '',
});

defineEmits<{
  'update:search': [value: string];
  'update:status': [value: string];
  clearSearch: [];
}>();
</script>

<template>
  <div class="flex min-w-0 flex-wrap items-center gap-3 rounded-xl border border-border bg-muted/30 p-3">
    <div class="min-w-56 flex-1">
      <SearchInput
        :model-value="search"
        :placeholder="searchPlaceholder"
        :label="searchLabel"
        :disabled="disabled"
        @update:model-value="$emit('update:search', $event)"
        @clear="$emit('clearSearch')"
      />
    </div>
    <Select
      v-if="statusOptions?.length"
      class="w-full sm:w-48"
      :model-value="status"
      :options="statusOptions"
      :placeholder="statusPlaceholder"
      :disabled="disabled"
      @update:model-value="$emit('update:status', $event)"
    />
    <slot />
    <div v-if="$slots.actions" class="ml-auto flex items-center gap-2">
      <slot name="actions" />
    </div>
  </div>
</template>
