<template>
  <div class="grid h-full min-h-0 grid-rows-page-bottom max-lg:h-auto max-lg:grid-rows-none">
    <!--
      WinUI editor host: internally scrolling body + fixed command bar footer.
      Page padding is owned by the parent ConsolePage (one padding per page),
      so neither the editor body nor the footer adds a page p-* layer: the
      footer is a full-bleed command bar (top divider + vertical spacing only).
      The desktop body scroll contract is unchanged.
    -->
    <div class="relative min-h-0 overflow-auto max-lg:overflow-visible"><slot /></div>
    <footer v-if="$slots.footer" class="border-t border-divider bg-layer py-3">
      <slot name="footer" />
    </footer>
  </div>
</template>
