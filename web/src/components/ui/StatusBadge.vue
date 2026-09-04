<script setup lang="ts">
import { computed } from 'vue';
import Badge from './Badge.vue';
import { resolveStatusTone, type StatusDomain, type StatusTone } from './statusTone';

const props = withDefaults(defineProps<{
  status: string;
  domain?: StatusDomain;
  label?: string;
  tone?: StatusTone;
}>(), {
  domain: 'generic',
});

const normalized = computed(() => props.status.trim().toLowerCase().replace(/\s+/g, '_'));
const resolvedTone = computed<StatusTone>(() => resolveStatusTone(props.domain, props.status, props.tone));
const resolvedLabel = computed(() => props.label ?? props.status);
</script>

<template>
  <Transition name="status" mode="out-in">
    <Badge :key="`${normalized}-${resolvedTone}`" :tone="resolvedTone">
      <slot :tone="resolvedTone" :label="resolvedLabel">{{ resolvedLabel }}</slot>
    </Badge>
  </Transition>
</template>