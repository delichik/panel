<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useI18n } from '@/i18n';
import { applicationsApi } from '@/api/applications';
import type { ApplicationDto } from '@/types/api';
import PageLoadingState from '@/components/PageLoadingState.vue';
import ApplicationEditor from './ApplicationEditor.vue';

const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const loading = ref(false);
const error = ref('');
const application = ref<ApplicationDto | null>(null);
let requestId = 0;

const applicationId = computed(() => (typeof route.params.applicationId === 'string' ? route.params.applicationId : ''));
const editing = computed(() => Boolean(applicationId.value));

function returnToApplications(applicationId?: string) {
  void router.push({
    path: '/applications/apps',
    query: applicationId ? { application: applicationId } : undefined,
  });
}

function handleSaved(app: ApplicationDto) {
  returnToApplications(app.id);
}

async function loadApplication() {
  const id = applicationId.value;
  const currentRequestId = ++requestId;
  error.value = '';
  if (!id) {
    application.value = null;
    loading.value = false;
    return;
  }
  loading.value = true;
  try {
    const loaded = await applicationsApi.get(id);
    if (currentRequestId !== requestId) return;
    application.value = loaded;
  } catch (err) {
    if (currentRequestId !== requestId) return;
    application.value = null;
    error.value = err instanceof Error ? err.message : t('applicationsPage.loadFailed');
  } finally {
    if (currentRequestId === requestId) loading.value = false;
  }
}

watch(applicationId, () => {
  void loadApplication();
});

onMounted(loadApplication);
</script>

<template>
  <div class="page-shell application-create-page">
    <div class="create-page-toolbar">
      <v-btn variant="text" prepend-icon="mdi-arrow-left" class="text-none" @click="returnToApplications(application?.id)">
        {{ t('applicationsPage.backToApplications') }}
      </v-btn>
    </div>
    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>
    <PageLoadingState v-else-if="loading" min-height="320px" />
    <ApplicationEditor
      v-else-if="!editing || application"
      :key="application?.id || 'create'"
      :application="application"
      :open="true"
      embedded
      @close="returnToApplications(application?.id)"
      @saved="handleSaved"
    />
  </div>
</template>

<style scoped>
.application-create-page {
  gap: 14px;
  overflow: hidden;
}

.create-page-toolbar {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  flex: 0 0 auto;
  min-width: 0;
  padding: 2px 4px;
}

@media (max-width: 760px) {
  .application-create-page {
    gap: 12px;
    overflow: visible;
  }
}
</style>
