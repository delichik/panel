<script setup lang="ts">
import { computed } from 'vue';
import type { RouteLocationRaw } from 'vue-router';
import { ArrowLeft } from '@/icons';
import BreadcrumbBar from '@/components/ui/BreadcrumbBar.vue';
import IconButton from '@/components/ui/IconButton.vue';
import { useI18n } from '@/i18n';

/** A breadcrumb entry: label + optional in-app target (WinUI BreadcrumbBar). */
interface BreadcrumbItem {
  label: string;
  to?: RouteLocationRaw;
}

const props = defineProps<{
  title: string;
  description?: string;
  backLabel?: string;
  /** Ancestor trail rendered above the title via the shared BreadcrumbBar. */
  breadcrumbs?: BreadcrumbItem[];
  /** Accessible name for the breadcrumb nav; defaults to t('common.breadcrumb'). */
  breadcrumbLabel?: string;
}>();
const emit = defineEmits<{ back: [] }>();
const { t } = useI18n();

const hasBreadcrumbs = computed(() => Boolean(props.breadcrumbs?.length));
const breadcrumbNavLabel = computed(() => props.breadcrumbLabel ?? t('common.breadcrumb'));
</script>

<template>
  <header class="flex shrink-0 items-center justify-between gap-4 max-lg:flex-col max-lg:items-stretch">
    <!--
      WinUI 3 page header as a pure structural block: no page padding here
      (ConsolePage owns the single page padding) so the header and the body
      always share one content column and one left edge.

      The back entry and the breadcrumb trail share one structural row above
      the title instead of being rendered in front of the h1: the title must
      start at the body's left edge on every page. The `#breadcrumb` slot is
      kept as an override/compat escape hatch; when it is provided it replaces
      the BreadcrumbBar rendering.

      Mobile (360–430px): the title balances/wraps by words instead of forcing
      a horizontal overflow, the description wraps as body text, the trail
      truncates inside its own column, and the action row stacks full width so
      buttons are never compressed or wrapped internally.

      The h1 is programmatically focusable (tabindex="-1") because the shell
      moves focus here after a page navigation; no focus ring for that move.
    -->
    <div class="min-w-0 flex-1">
      <div
        v-if="backLabel || $slots.breadcrumb || hasBreadcrumbs"
        class="mb-2 flex min-w-0 flex-wrap items-center gap-2 max-sm:gap-1.5"
      >
        <IconButton v-if="backLabel" size="sm" variant="ghost" :label="backLabel" @click="emit('back')">
          <ArrowLeft />
        </IconButton>
        <slot name="breadcrumb" />
        <BreadcrumbBar
          v-if="!$slots.breadcrumb && hasBreadcrumbs"
          class="min-w-0 flex-1"
          :items="breadcrumbs ?? []"
          :label="breadcrumbNavLabel"
        />
      </div>
      <h1 tabindex="-1" class="m-0 text-balance break-words text-title font-semibold tracking-tight text-foreground focus:outline-none">{{ title }}</h1>
      <p v-if="description" class="m-0 mt-0.5 max-w-3xl break-words text-body text-secondary-foreground">{{ description }}</p>
    </div>
    <div
      v-if="$slots.actions"
      class="flex min-w-0 max-w-full shrink-0 flex-wrap items-center justify-end gap-2 max-lg:w-full max-lg:justify-start max-sm:flex-col max-sm:items-stretch max-sm:gap-2 max-sm:[&>*]:w-full"
    >
      <slot name="actions" />
    </div>
  </header>
</template>
