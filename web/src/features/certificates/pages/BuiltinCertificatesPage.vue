<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { certificatesApi } from '@/api/certificates';
import { useI18n } from '@/i18n';
import type { NomadBuiltinCertificateDto } from '@/types/api';
import PageLoadingState from '@/components/PageLoadingState.vue';

const { t, formatDateTime } = useI18n();
const certificates = ref<NomadBuiltinCertificateDto[]>([]);
const loading = ref(false);
const rotating = ref(false);
const confirmDialog = ref(false);
const error = ref('');
const taskId = ref('');

async function load() {
  loading.value = true;
  try {
    certificates.value = await certificatesApi.builtin();
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('builtinCertificates.loadFailed');
  } finally {
    loading.value = false;
  }
}

async function rotate() {
  rotating.value = true;
  try {
    const result = await certificatesApi.rotateBuiltin();
    taskId.value = result.taskId;
    confirmDialog.value = false;
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('builtinCertificates.rotateFailed');
  } finally {
    rotating.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
    <v-alert v-if="taskId" type="info" variant="tonal" class="mb-4">
      {{ t('builtinCertificates.rotateQueued') }}
      <router-link :to="{ path: '/tasks', query: { task: taskId } }">{{ t('common.viewTask') }}</router-link>
    </v-alert>
    <v-card variant="outlined">
      <div class="app-card-header">
        <div>
          <div class="text-h6">{{ t('builtinCertificates.title') }}</div>
          <div class="text-body-2 text-medium-emphasis">{{ t('builtinCertificates.subtitle') }}</div>
        </div>
        <v-btn color="error" variant="outlined" prepend-icon="mdi-autorenew" @click="confirmDialog = true">{{ t('builtinCertificates.rotate') }}</v-btn>
      </div>
      <PageLoadingState v-if="loading" min-height="220px" />
      <v-table v-else density="comfortable">
        <thead><tr><th>{{ t('common.name') }}</th><th>{{ t('common.type') }}</th><th>{{ t('builtinCertificates.fingerprint') }}</th><th>{{ t('builtinCertificates.validUntil') }}</th></tr></thead>
        <tbody>
          <tr v-for="certificate in certificates" :key="certificate.id">
            <td>{{ certificate.name }}</td>
            <td>{{ certificate.kind }}</td>
            <td class="mono text-truncate">{{ certificate.fingerprint }}</td>
            <td>{{ formatDateTime(certificate.notAfter) }}</td>
          </tr>
        </tbody>
      </v-table>
    </v-card>

    <v-dialog v-model="confirmDialog" max-width="620">
      <v-card>
        <v-card-title>{{ t('builtinCertificates.confirmTitle') }}</v-card-title>
        <v-card-text>
          <v-alert type="warning" variant="tonal">{{ t('builtinCertificates.confirmMessage') }}</v-alert>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="confirmDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="error" variant="flat" :loading="rotating" @click="rotate">{{ t('builtinCertificates.confirmRotate') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.mono { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; max-width: 360px; }
</style>
