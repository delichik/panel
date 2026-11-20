<template>
  <div class="grid h-full min-h-0 min-w-0 grid-rows-page max-lg:h-auto max-lg:grid-rows-none">
    <!--
      WinUI full-height workspace (diagnostics / logs / terminal): toolbar row on
      top of an internally scrolling content region. Page padding is owned by the
      parent ConsolePage, so the workspace adds no p-* of its own. Below the
      desktop breakpoint the shell restores page-level scrolling, so the template
      must release its viewport clipping instead of nesting a fixed-height scroll
      region into a scrolling page.
    -->
    <div v-if="$slots.toolbar" class="border-b border-divider"><slot name="toolbar" /></div>
    <div class="min-h-0 min-w-0 overflow-auto max-lg:overflow-visible"><slot /></div>
  </div>
</template>
