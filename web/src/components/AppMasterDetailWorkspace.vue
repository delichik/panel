<template>
  <div class="app-master-detail-workspace">
    <slot name="aside" />
    <slot />
  </div>
</template>

<style scoped>
.app-master-detail-workspace {
  display: grid;
  grid-template-columns: clamp(300px, 26vw, 340px) minmax(0, 1fr);
  flex: 1 1 auto;
  gap: 18px;
  align-items: stretch;
  min-height: 0;
  min-width: 0;
}

.app-master-detail-workspace > :deep(*) {
  min-width: 0;
}

@media (max-width: 1080px) {
  .app-master-detail-workspace {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 760px) {
  .app-master-detail-workspace {
    flex: none;
    min-height: auto;
  }
}
</style>
