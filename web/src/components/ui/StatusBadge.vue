<script setup lang="ts">
import { computed } from 'vue';
import Badge from './Badge.vue';

type Tone = 'neutral' | 'success' | 'warning' | 'danger' | 'info';
type Domain = 'server' | 'task' | 'certificate' | 'resource' | 'operation' | 'generic';

const props = withDefaults(defineProps<{
  status: string;
  domain?: Domain;
  label?: string;
  tone?: Tone;
}>(), {
  domain: 'generic',
});

const toneMaps: Record<Domain, Record<string, Tone>> = {
  generic: {
    active: 'success',
    enabled: 'success',
    ready: 'success',
    healthy: 'success',
    success: 'success',
    ok: 'success',
    pending: 'warning',
    warning: 'warning',
    degraded: 'warning',
    disabled: 'neutral',
    inactive: 'neutral',
    unknown: 'neutral',
    error: 'danger',
    failed: 'danger',
    danger: 'danger',
    running: 'info',
    processing: 'info',
  },
  server: {
    online: 'success',
    reachable: 'success',
    connected: 'success',
    installing: 'info',
    updating: 'info',
    warning: 'warning',
    degraded: 'warning',
    offline: 'danger',
    unreachable: 'danger',
    disabled: 'neutral',
    unknown: 'neutral',
  },
  task: {
    succeeded: 'success',
    success: 'success',
    completed: 'success',
    running: 'info',
    queued: 'info',
    pending: 'warning',
    retrying: 'warning',
    failed: 'danger',
    cancelled: 'neutral',
    skipped: 'neutral',
  },
  certificate: {
    valid: 'success',
    active: 'success',
    issuing: 'info',
    renewing: 'info',
    expiring: 'warning',
    expired: 'danger',
    failed: 'danger',
    revoked: 'danger',
    unknown: 'neutral',
  },
  resource: {
    running: 'success',
    active: 'success',
    available: 'success',
    pulling: 'info',
    updating: 'info',
    stale: 'warning',
    paused: 'warning',
    stopped: 'neutral',
    unused: 'neutral',
    failed: 'danger',
    error: 'danger',
  },
  operation: {
    accepted: 'info',
    queued: 'info',
    running: 'info',
    succeeded: 'success',
    success: 'success',
    completed: 'success',
    warning: 'warning',
    needs_attention: 'warning',
    partial_failed: 'warning',
    failed: 'danger',
    blocked: 'danger',
    cancelled: 'neutral',
  },
};

const normalized = computed(() => props.status.trim().toLowerCase().replace(/\s+/g, '_'));
const resolvedTone = computed<Tone>(() => props.tone ?? toneMaps[props.domain][normalized.value] ?? toneMaps.generic[normalized.value] ?? 'neutral');
const resolvedLabel = computed(() => props.label ?? props.status);
</script>

<template>
  <Transition name="status" mode="out-in">
    <Badge :key="`${normalized}-${resolvedTone}`" :tone="resolvedTone">
      <slot :tone="resolvedTone" :label="resolvedLabel">{{ resolvedLabel }}</slot>
    </Badge>
  </Transition>
</template>
