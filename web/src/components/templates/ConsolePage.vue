<script setup lang="ts">
import { computed } from 'vue';
import type { RouteLocationRaw } from 'vue-router';
import PageHeader from '@/components/shell/PageHeader.vue';

/** A breadcrumb entry: label + optional in-app target (WinUI BreadcrumbBar). */
interface BreadcrumbItem {
  label: string;
  to?: RouteLocationRaw;
}

const props = withDefaults(defineProps<{
  title: string;
  description?: string;
  backLabel?: string;
  /**
   * Scroll the body internally (default). Pages that manage their own region
   * scrolling (for example a tabbed covered detail with tables) pass false and
   * keep every panel's scrolling inside its own content area.
   */
  bodyScroll?: boolean;
  /**
   * Page content column width. `content` = WinUI data/list page (1200px),
   * `narrow` = settings/form page (1000px). The column is centered in the
   * shell content area and the header + body always share the same width.
   */
  width?: 'content' | 'narrow';
  /** Ancestor trail above the page title (shared WinUI BreadcrumbBar). */
  breadcrumbs?: BreadcrumbItem[];
  /** Accessible name for the breadcrumb nav; defaults to t('common.breadcrumb'). */
  breadcrumbLabel?: string;
}>(), { bodyScroll: true, width: 'content', breadcrumbs: undefined, breadcrumbLabel: undefined });
const emit = defineEmits<{ back: [] }>();

const widthClass = computed(() => (props.width === 'narrow' ? 'max-w-content-narrow' : 'max-w-content'));
</script>

<template>
  <div
    class="mx-auto grid h-full min-h-0 w-full min-w-0 grid-rows-page gap-3 p-4 max-lg:block max-lg:p-3 max-lg:min-h-[calc(100dvh-var(--panel-shell-header))]"
    :class="widthClass"
    :data-width="width"
  >
    <!--
      WinUI 3 content container: header row + internal scroll body. The root
      stays transparent so the shell content layer (and Mica underneath) shows
      through; the body never draws its own opaque page background.

      ConsolePage is the single page-padding owner (p-4, narrow max-lg:p-3):
      PageHeader is a structural header without padding, and templates composed
      inside the body (ListPage/EditorPage/WorkspacePage) must not add another
      page p-* layer. Padding lives on the root so the header and body always
      share one column and one left edge.

      Breadcrumbs are passed through to PageHeader; pages that need a custom
      trail can still use the #breadcrumb slot as an override.
    -->
    <PageHeader
      :title="title"
      :description="description"
      :back-label="backLabel"
      :breadcrumbs="breadcrumbs"
      :breadcrumb-label="breadcrumbLabel"
      @back="emit('back')"
    >
      <template v-if="$slots.actions" #actions><slot name="actions" /></template>
      <template v-if="$slots.breadcrumb" #breadcrumb><slot name="breadcrumb" /></template>
    </PageHeader>
    <div
      class="min-h-0 min-w-0 max-lg:mt-3 max-lg:overflow-visible"
      :class="bodyScroll ? 'overflow-y-auto overflow-x-hidden' : 'overflow-hidden'"
    >
      <slot />
    </div>
  </div>
</template>
