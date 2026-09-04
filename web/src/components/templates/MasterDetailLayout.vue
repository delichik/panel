<template>
  <div class="grid min-h-0 min-w-0 grid-cols-1 gap-4 overflow-x-hidden xl:grid-cols-[360px_minmax(0,1fr)]">
    <slot name="master" />
    <slot name="detail" />
  </div>
</template>
