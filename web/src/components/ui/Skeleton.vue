<script setup lang="ts">
import { computed, useAttrs } from 'vue';
import { cn } from './cn';

const attrs = useAttrs();
const classes = computed(() => cn('motion-skeleton animate-pulse rounded-xl bg-muted', attrs.class as string));
</script>

<template>
  <div aria-hidden="true" :class="classes" />
</template>
