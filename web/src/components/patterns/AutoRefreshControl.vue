<script setup lang="ts">
import { Check, ChevronDown, RefreshCcw } from '@lucide/vue';
import Button from '@/components/ui/Button.vue';
import Dropdown from '@/components/ui/Dropdown.vue';
import DropdownItem from '@/components/ui/DropdownItem.vue';
import { useAutoRefresh, type AutoRefreshMode } from '@/composables/useAutoRefresh';

defineProps<{
  offLabel: string;
  shortLabel: string;
  longLabel: string;
  hintLabel: string;
}>();

const { mode, setMode } = useAutoRefresh();

function select(value: AutoRefreshMode) {
  setMode(value);
}
</script>

<template>
  <Dropdown align="right">
    <template #trigger>
      <Button size="sm" variant="secondary" :title="hintLabel">
        <RefreshCcw class="size-4" />
        <span>{{ mode === 'off' ? offLabel : mode === '5' ? shortLabel : longLabel }}</span>
        <ChevronDown class="size-3.5 text-muted-foreground" />
      </Button>
    </template>
    <DropdownItem :class="mode === 'off' ? 'text-brand font-semibold' : ''" @click="select('off')">
      <span class="flex-1">{{ offLabel }}</span>
      <span class="flex w-4 justify-end"><Check v-if="mode === 'off'" class="size-4 text-brand" /></span>
    </DropdownItem>
    <DropdownItem :class="mode === '5' ? 'text-brand font-semibold' : ''" @click="select('5')">
      <span class="flex-1">{{ shortLabel }}</span>
      <span class="flex w-4 justify-end"><Check v-if="mode === '5'" class="size-4 text-brand" /></span>
    </DropdownItem>
    <DropdownItem :class="mode === '10' ? 'text-brand font-semibold' : ''" @click="select('10')">
      <span class="flex-1">{{ longLabel }}</span>
      <span class="flex w-4 justify-end"><Check v-if="mode === '10'" class="size-4 text-brand" /></span>
    </DropdownItem>
  </Dropdown>
</template>
