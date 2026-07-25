<template>
  <div class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)]">
    <slot name="header" />
    <div class="min-h-0 overflow-auto p-6 max-sm:p-4">
      <div class="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(280px,1fr))]">
        <slot />
      </div>
    </div>
  </div>
</template>
