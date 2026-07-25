<template>
  <div class="grid h-full min-h-0 grid-cols-[300px_minmax(0,1fr)] gap-4 max-lg:block">
    <aside class="min-h-0 overflow-auto rounded-2xl border border-border bg-card max-lg:mb-4">
      <slot name="master" />
    </aside>
    <section class="min-h-0 overflow-auto rounded-2xl border border-border bg-card">
      <slot name="detail" />
    </section>
  </div>
</template>
