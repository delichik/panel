<script setup lang="ts">
import { computed } from 'vue';
import { Check, ChevronDown, RefreshCcw } from '@/icons';
import Button from '@/components/ui/Button.vue';
import Dropdown from '@/components/ui/Dropdown.vue';
import DropdownItem from '@/components/ui/DropdownItem.vue';
import { useAutoRefresh, type AutoRefreshMode } from '@/composables/useAutoRefresh';

const props = defineProps<{
  offLabel: string;
  options: Array<{ value: AutoRefreshMode; label: string }>;
  hintLabel: string;
}>();

const { mode, setMode } = useAutoRefresh();
const currentLabel = computed(() => (
  mode.value === 'off' ? props.offLabel : (props.options.find((option) => option.value === mode.value)?.label ?? '')
));

function select(value: AutoRefreshMode) {
  setMode(value);
}
</script>

<template>
  <Dropdown align="right">
    <template #trigger>
      <Button size="sm" variant="secondary" :title="hintLabel">
        <RefreshCcw class="size-4" />
        <span>{{ currentLabel }}</span>
        <ChevronDown class="size-4 text-muted-foreground" />
      </Button>
    </template>
    <!-- MenuFlyout 选中语义：只显示勾选，不改文字颜色/字重。 -->
    <DropdownItem @click="select('off')">
      <span class="flex-1">{{ offLabel }}</span>
      <span class="flex w-4 justify-end"><Check v-if="mode === 'off'" class="size-4 text-accent-text" aria-hidden="true" /></span>
    </DropdownItem>
    <DropdownItem
      v-for="option in options"
      :key="option.value"
      @click="select(option.value)"
    >
      <span class="flex-1">{{ option.label }}</span>
      <span class="flex w-4 justify-end"><Check v-if="mode === option.value" class="size-4 text-accent-text" aria-hidden="true" /></span>
    </DropdownItem>
  </Dropdown>
</template>
