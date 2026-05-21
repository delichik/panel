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
    const tab = String(route.query.tab || 'services');
    return ['templates', 'services', 'runtime'].includes(tab) ? tab : 'services';
  },
  set(tab: string) {
    void router.replace({ path: '/docker', query: { ...route.query, tab } });
  },
});
</script>

<template>
  <div class="docker-page">
    <v-card variant="outlined" class="mb-4">
      <v-tabs v-model="activeTab" color="primary" border-bottom>
        <v-tab value="services" class="text-none font-weight-bold">Services</v-tab>
        <v-tab value="runtime" class="text-none font-weight-bold">Runtime Resources</v-tab>
        <v-tab value="templates" class="text-none font-weight-bold">Service Templates</v-tab>
      </v-tabs>
    </v-card>

    <ServicesPage v-if="activeTab === 'services'" />
    <DockerRuntimePage v-else-if="activeTab === 'runtime'" />
    <ServiceTemplatesPage v-else />
  </div>
</template>

<style scoped>
.docker-page {
  display: flex;
  flex-direction: column;
}
</style>
