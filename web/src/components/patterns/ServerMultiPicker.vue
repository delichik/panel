<script setup lang="ts">
import { Check } from '@lucide/vue';
import { computed } from 'vue';
import StatusBadge from '../ui/StatusBadge.vue';

type ServerOption = {
  id: string;
  name: string;
  description?: string;
  status?: string;
  capabilities?: string[];
};

const props = withDefaults(defineProps<{
  modelValue: string[];
  servers: ServerOption[];
  disabled?: boolean;
  disabledIds?: string[];
  disabledReasons?: Record<string, string>;
  label?: string;
}>(), {
  disabledIds: () => [],
  disabledReasons: () => ({}),
});

const emit = defineEmits<{ 'update:modelValue': [value: string[]] }>();

const selected = computed(() => new Set(props.modelValue));
const blocked = computed(() => new Set(props.disabledIds));

function toggle(id: string) {
  if (props.disabled || blocked.value.has(id)) return;
  const next = new Set(props.modelValue);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  emit('update:modelValue', Array.from(next));
}
</script>

<template>
  <section class="grid gap-2 motion-stagger" :aria-label="label">
    <button
      v-for="server in servers"
      :key="server.id"
      type="button"
      :disabled="disabled || blocked.has(server.id)"
      :aria-pressed="selected.has(server.id)"
      class="motion-list-item grid min-h-20 w-full gap-2 rounded-xl border border-border bg-card p-3 text-left transition-colors hover:bg-accent/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 disabled:cursor-not-allowed disabled:opacity-50 aria-pressed:border-primary aria-pressed:bg-primary/10"
      @click="toggle(server.id)"
    >
      <span class="flex min-w-0 items-start gap-3">
        <span class="mt-0.5 grid size-5 shrink-0 place-items-center rounded-md border border-input bg-muted text-xs font-semibold text-primary" aria-hidden="true">
          <Check v-if="selected.has(server.id)" class="size-3.5" />
        </span>
        <span class="min-w-0 flex-1">
          <span class="flex min-w-0 items-start justify-between gap-2">
            <span class="truncate text-sm font-semibold text-foreground">{{ server.name }}</span>
            <StatusBadge v-if="server.status" :status="server.status" domain="server" />
          </span>
          <span v-if="server.description" class="mt-1 block line-clamp-2 text-xs leading-5 text-muted-foreground">{{ server.description }}</span>
          <span v-if="server.capabilities?.length || disabledReasons[server.id]" class="mt-2 flex flex-wrap gap-1.5">
            <span v-for="capability in server.capabilities" :key="capability" class="rounded-full border border-border bg-muted px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
              {{ capability }}
            </span>
            <span v-if="disabledReasons[server.id]" class="text-xs text-danger">{{ disabledReasons[server.id] }}</span>
          </span>
        </span>
      </span>
    </button>
  </section>
</template>
