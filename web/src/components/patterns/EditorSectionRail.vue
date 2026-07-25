<script setup lang="ts">
import { AlertTriangle, CheckCircle2, Circle, Dot } from '@lucide/vue';
import Badge from '../ui/Badge.vue';

type RailChild = {
  id: string;
  label: string;
  status?: string;
  disabled?: boolean;
};

type RailSection = {
  id: string;
  label: string;
  description?: string;
  complete?: boolean;
  error?: boolean;
  dirty?: boolean;
  disabled?: boolean;
  children?: RailChild[];
};

defineProps<{
  modelValue: string;
  sections: RailSection[];
  childValue?: string;
  label?: string;
  completeLabel: string;
  errorLabel: string;
  dirtyLabel: string;
}>();

defineEmits<{
  'update:modelValue': [value: string];
  'update:childValue': [value: string];
}>();
</script>

<template>
  <nav class="grid gap-2" :aria-label="label">
    <div v-for="section in sections" :key="section.id" class="grid gap-1">
      <button
        type="button"
        :disabled="section.disabled"
        :aria-current="modelValue === section.id ? 'true' : undefined"
        class="motion-list-item grid w-full gap-1 rounded-xl border border-border bg-background p-3 text-left transition-colors hover:bg-accent/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 disabled:cursor-not-allowed disabled:opacity-50 aria-current:border-primary aria-current:bg-primary/10"
        @click="$emit('update:modelValue', section.id)"
      >
        <span class="flex min-w-0 items-center gap-2">
          <AlertTriangle v-if="section.error" class="size-4 shrink-0 text-danger" aria-hidden="true" />
          <CheckCircle2 v-else-if="section.complete" class="size-4 shrink-0 text-success" aria-hidden="true" />
          <Circle v-else class="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          <span class="min-w-0 flex-1 truncate text-sm font-semibold text-foreground">{{ section.label }}</span>
          <Badge v-if="section.error" tone="danger">{{ errorLabel }}</Badge>
          <Badge v-else-if="section.dirty" tone="warning">{{ dirtyLabel }}</Badge>
          <Badge v-else-if="section.complete" tone="success">{{ completeLabel }}</Badge>
        </span>
        <span v-if="section.description" class="line-clamp-2 pl-6 text-xs leading-5 text-muted-foreground">{{ section.description }}</span>
      </button>
      <div v-if="section.children?.length" class="grid gap-1 pl-5">
        <button
          v-for="child in section.children"
          :key="child.id"
          type="button"
          :disabled="section.disabled || child.disabled"
          :aria-current="childValue === child.id ? 'true' : undefined"
          class="motion-menu-item flex h-8 min-w-0 items-center gap-1.5 rounded-lg px-2 text-left text-xs font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 disabled:cursor-not-allowed disabled:opacity-50 aria-current:bg-background aria-current:text-foreground aria-current:shadow-sm"
          @click="$emit('update:modelValue', section.id); $emit('update:childValue', child.id)"
        >
          <Dot class="size-4 shrink-0" aria-hidden="true" />
          <span class="min-w-0 flex-1 truncate">{{ child.label }}</span>
          <Badge v-if="child.status" tone="neutral">{{ child.status }}</Badge>
        </button>
      </div>
    </div>
  </nav>
</template>
