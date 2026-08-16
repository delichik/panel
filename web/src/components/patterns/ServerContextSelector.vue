<script setup lang="ts">
import { Check } from '@lucide/vue';
import StatusBadge from '../ui/StatusBadge.vue';

type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info';

type ServerOption = {
  id: string;
  name: string;
  description?: string;
  status?: string;
  statusLabel?: string;
  statusTone?: Tone;
  capabilities?: string[];
  disabled?: boolean;
  disabledReason?: string;
};

withDefaults(defineProps<{
  modelValue?: string;
  servers: ServerOption[];
  label?: string;
  disabled?: boolean;
  loading?: boolean;
  loadingRows?: number;
}>(), {
  modelValue: '',
  loading: false,
  loadingRows: 5,
});

defineEmits<{ 'update:modelValue': [value: string] }>();
</script>

<template>
  <section class="grid gap-2 motion-stagger" role="radiogroup" :aria-label="label" :aria-busy="loading ? 'true' : undefined">
    <div v-if="loading && !servers.length" class="grid gap-2">
      <div v-for="item in loadingRows" :key="item" class="grid min-h-20 gap-2 rounded-xl border border-border bg-card p-3" aria-hidden="true">
        <div class="flex items-start gap-3">
          <div class="motion-skeleton size-5 rounded-md bg-muted animate-pulse" />
          <div class="min-w-0 flex-1">
            <div class="motion-skeleton h-4 w-32 rounded bg-muted animate-pulse" />
            <div class="motion-skeleton mt-2 h-3 w-48 max-w-full rounded bg-muted animate-pulse" />
            <div class="mt-3 flex gap-1.5">
              <div class="motion-skeleton h-5 w-14 rounded-full bg-muted animate-pulse" />
              <div class="motion-skeleton h-5 w-16 rounded-full bg-muted animate-pulse" />
            </div>
          </div>
        </div>
      </div>
    </div>
    <template v-else>
      <button
        v-for="server in servers"
        :key="server.id"
        type="button"
        role="radio"
        :disabled="disabled || server.disabled"
        :aria-checked="modelValue === server.id"
        class="motion-list-item grid min-h-20 w-full gap-2 rounded-xl border border-border bg-card p-3 text-left transition-colors hover:bg-accent/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 disabled:cursor-not-allowed disabled:opacity-50 aria-checked:border-primary aria-checked:bg-primary/10"
        @click="$emit('update:modelValue', server.id)"
      >
        <span class="flex min-w-0 items-start gap-3">
          <span class="mt-0.5 grid size-5 shrink-0 place-items-center rounded-md border border-input bg-muted text-xs font-semibold text-primary" aria-hidden="true">
            <Check v-if="modelValue === server.id" class="size-3.5" />
          </span>
          <span class="min-w-0 flex-1">
            <span class="flex min-w-0 items-start justify-between gap-2">
              <span class="truncate text-sm font-semibold text-foreground">{{ server.name }}</span>
              <StatusBadge v-if="server.status" class="shrink-0" :status="server.status" :label="server.statusLabel" :tone="server.statusTone" domain="server" />
            </span>
            <span v-if="server.description" class="mt-1 block line-clamp-2 text-xs leading-5 text-muted-foreground">{{ server.description }}</span>
            <span v-if="server.capabilities?.length || server.disabledReason" class="mt-2 flex flex-wrap items-center gap-1.5">
              <span v-for="capability in server.capabilities" :key="capability" class="rounded-full border border-border bg-muted px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
                {{ capability }}
              </span>
              <span v-if="server.disabledReason" class="text-xs text-danger">{{ server.disabledReason }}</span>
            </span>
          </span>
        </span>
      </button>
    </template>
  </section>
</template>
