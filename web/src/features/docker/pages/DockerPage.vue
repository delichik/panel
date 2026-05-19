<script setup lang="ts">
import { computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import DockerRuntimePage from './DockerRuntimePage.vue';
import ServiceTemplatesPage from '@/features/compose/pages/ServiceTemplatesPage.vue';
import ServicesPage from '@/features/compose/pages/ServicesPage.vue';

const route = useRoute();
const router = useRouter();

const activeTab = computed({
  get() {
    const tab = String(route.query.tab || 'templates');
    return ['templates', 'services', 'runtime'].includes(tab) ? tab : 'templates';
  },
  set(tab: string) {
    void router.replace({ path: '/docker', query: { ...route.query, tab } });
  },
});
</script>

<template>
  <div class="docker-page">
    <section class="panel docker-nav">
      <el-tabs v-model="activeTab">
        <el-tab-pane label="Service Templates" name="templates" />
        <el-tab-pane label="Services" name="services" />
        <el-tab-pane label="Runtime Resources" name="runtime" />
      </el-tabs>
    </section>

    <ServiceTemplatesPage v-if="activeTab === 'templates'" />
    <ServicesPage v-else-if="activeTab === 'services'" />
    <DockerRuntimePage v-else />
  </div>
</template>

<style scoped>
.docker-page {
  display: grid;
  gap: 20px;
}

.docker-nav {
  padding: 0 20px;
}

.docker-nav :deep(.el-tabs__header) {
  margin-bottom: 0;
}
</style>
