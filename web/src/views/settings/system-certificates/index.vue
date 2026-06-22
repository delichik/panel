<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import AppSelectorItem from '@/components/AppSelectorItem.vue';
import AppSelectorPanel from '@/components/AppSelectorPanel.vue';
import PageLoadingState from '@/components/PageLoadingState.vue';
import { keyAssetsApi } from '@/api/keyAssets';
import { useI18n } from '@/i18n';
import type { SystemCertificateDto } from '@/types/api';

const { formatDateTime, t } = useI18n();
const certificates = ref<SystemCertificateDto[]>([]);
const selectedId = ref('');
const loading = ref(false);
const error = ref('');
const resetTarget = ref<SystemCertificateDto | null>(null);
const resetDialog = ref(false);
const resetAcknowledged = ref(false);
const resetting = ref(false);
const snackbar = ref(false);
const lastTaskId = ref('');

const selectedCertificate = computed(() => certificates.value.find((item) => item.id === selectedId.value) ?? null);

function certificateName(item: SystemCertificateDto) {
  if (item.id === 'agent-ca') return t('keyAssetsPage.agentCaName');
  if (item.id === 'agent-panel-client') return t('keyAssetsPage.agentClientName');
  if (item.serverName) return t('keyAssetsPage.agentServerName', { name: item.serverName });
  return item.name;
}

function typeLabel(value: string) {
  return value === 'ca_certificate' ? t('keyAssetsPage.typeCa') : t('keyAssetsPage.typeTls');
}

function statusLabel(item: SystemCertificateDto) {
  if (item.status === 'valid' || item.status === 'compatible') return t('keyAssetsPage.systemStatusValid');
  if (item.status === 'expired') return t('keyAssetsPage.systemStatusExpired');
  if (item.status === 'not_yet_valid') return t('keyAssetsPage.systemStatusNotYetValid');
  if (item.status === 'incompatible') return t('keyAssetsPage.systemStatusIncompatible');
  if (item.status === 'unavailable') return t('keyAssetsPage.systemStatusUnavailable');
  return t('common.unknown');
}

function statusColor(item: SystemCertificateDto) {
  if (item.status === 'valid' || item.status === 'compatible') return 'success';
  if (item.status === 'expired' || item.status === 'incompatible' || item.status === 'unavailable') return 'error';
  return 'warning';
}

function formatDate(value?: string | null) {
  return value ? formatDateTime(value) : t('common.notAvailable');
}

async function load() {
  loading.value = true;
  try {
    certificates.value = await keyAssetsApi.listSystemCertificates();
    if (!certificates.value.some((item) => item.id === selectedId.value)) {
      selectedId.value = certificates.value[0]?.id ?? '';
    }
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('keyAssetsPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

function askReset(item: SystemCertificateDto) {
  resetTarget.value = item;
  resetAcknowledged.value = false;
  resetDialog.value = true;
}

async function resetCertificate() {
  if (!resetTarget.value || !resetAcknowledged.value) return;
  resetting.value = true;
  try {
    const result = await keyAssetsApi.resetSystemCertificate(resetTarget.value.id);
    lastTaskId.value = result.taskId;
    resetDialog.value = false;
    snackbar.value = true;
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('keyAssetsPage.createFailed');
  } finally {
    resetting.value = false;
  }
}

function taskRoute() {
  return lastTaskId.value ? { path: '/tasks', query: { task: lastTaskId.value } } : '/tasks';
}

onMounted(load);
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal">{{ error }}</v-alert>

    <div class="system-certificates-workspace">
      <AppSelectorPanel
        :title="t('layout.nav.settingsSystemCertificates')"
        :loading="loading"
        :empty="certificates.length === 0"
        empty-icon="mdi-shield-lock-outline"
        :empty-text="t('keyAssetsPage.noSystemCertificates')"
      >
        <AppSelectorItem
          v-for="certificate in certificates"
          :key="certificate.id"
          :selected="certificate.id === selectedId"
          @select="selectedId = certificate.id"
        >
          <span class="selector-main min-width-0">
            <v-icon size="20" color="medium-emphasis">mdi-shield-lock-outline</v-icon>
            <span class="selector-copy min-width-0">
              <span class="selector-name text-truncate">{{ certificateName(certificate) }}</span>
              <span class="selector-meta text-truncate">{{ certificate.commonName || certificate.serverName || t('common.notAvailable') }}</span>
            </span>
          </span>
          <v-chip size="x-small" :color="statusColor(certificate)" variant="tonal">{{ statusLabel(certificate) }}</v-chip>
        </AppSelectorItem>
      </AppSelectorPanel>

      <v-card variant="outlined" class="system-certificate-detail">
        <PageLoadingState v-if="loading && !selectedCertificate" min-height="300px" />
        <template v-else-if="selectedCertificate">
          <div class="detail-header">
            <div class="min-width-0">
              <div class="text-h6 font-weight-bold text-truncate">{{ certificateName(selectedCertificate) }}</div>
              <div class="text-body-2 text-medium-emphasis">{{ typeLabel(selectedCertificate.type) }}</div>
            </div>
            <div class="detail-actions">
              <v-chip size="small" color="info" variant="tonal">{{ t('keyAssetsPage.systemBuiltIn') }}</v-chip>
              <v-chip size="small" :color="statusColor(selectedCertificate)" variant="tonal">{{ statusLabel(selectedCertificate) }}</v-chip>
              <v-btn size="small" color="warning" variant="outlined" :disabled="!selectedCertificate.canReset" @click="askReset(selectedCertificate)">
                {{ t('keyAssetsPage.systemReset') }}
              </v-btn>
            </div>
          </div>
          <div class="detail-body">
            <div class="detail-grid">
              <div><span>{{ t('common.type') }}</span><strong>{{ typeLabel(selectedCertificate.type) }}</strong></div>
              <div><span>{{ t('keyAssetsPage.commonName') }}</span><strong>{{ selectedCertificate.commonName || t('common.notAvailable') }}</strong></div>
              <div><span>{{ t('common.fingerprint') }}</span><strong class="mono">{{ selectedCertificate.fingerprint || t('common.notAvailable') }}</strong></div>
              <div><span>{{ t('common.validUntil') }}</span><strong>{{ formatDate(selectedCertificate.notAfter) }}</strong></div>
              <div><span>{{ t('common.status') }}</span><strong>{{ statusLabel(selectedCertificate) }}</strong></div>
              <div><span>{{ t('serversPage.servers') }}</span><strong>{{ selectedCertificate.serverName || t('common.notAvailable') }}</strong></div>
            </div>
          </div>
        </template>
        <div v-else class="empty-detail text-medium-emphasis">{{ t('keyAssetsPage.noSystemCertificates') }}</div>
      </v-card>
    </div>

    <v-dialog v-model="resetDialog" width="560">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('keyAssetsPage.systemResetTitle') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="resetDialog = false" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <p class="confirm-message">
            {{ resetTarget?.id === 'agent-ca'
              ? t('keyAssetsPage.systemResetCaMessage')
              : t('keyAssetsPage.systemResetMessage', { name: resetTarget ? certificateName(resetTarget) : '' }) }}
          </p>
          <v-checkbox v-model="resetAcknowledged" color="error" :label="t('keyAssetsPage.dangerAcknowledge')" class="mt-4" />
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" @click="resetDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="error" variant="flat" :disabled="!resetAcknowledged" :loading="resetting" @click="resetCertificate">
            {{ t('keyAssetsPage.systemReset') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar" color="info">
      {{ t('keyAssetsPage.systemResetQueued') }}
      <template #actions>
        <v-btn color="white" variant="text" :to="taskRoute()">{{ t('common.viewTask') }}</v-btn>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.system-certificates-workspace {
  display: grid;
  grid-template-columns: clamp(300px, 26vw, 340px) minmax(0, 1fr);
  flex: 1 1 auto;
  gap: 18px;
  min-height: 0;
}

.system-certificate-detail {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}

.selector-main { display: flex; align-items: center; gap: 9px; min-width: 0; }
.selector-copy, .selector-name, .selector-meta { display: block; min-width: 0; }
.selector-name { font-size: 0.9rem; font-weight: 700; }
.selector-meta { margin-top: 2px; color: var(--lp-text-muted); font-size: 0.76rem; }
.detail-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; padding: 16px; border-bottom: 1px solid var(--lp-border); }
.detail-actions { display: flex; align-items: center; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }
.detail-body { min-height: 0; padding: 16px; overflow: auto; }
.detail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
.detail-grid > div { display: grid; gap: 4px; min-width: 0; padding: 12px; border: 1px solid var(--lp-border); border-radius: 8px; }
.detail-grid span { color: var(--lp-text-muted); font-size: 0.76rem; }
.detail-grid strong { overflow-wrap: anywhere; font-size: 0.88rem; }
.empty-detail { display: grid; flex: 1 1 auto; place-items: center; min-height: 220px; padding: 32px; }
.mono { font-family: "Cascadia Code", "SFMono-Regular", Consolas, monospace; }
.min-width-0 { min-width: 0; }
.confirm-message { margin: 0; line-height: 1.55; white-space: pre-wrap; }

@media (max-width: 1080px) {
  .system-certificates-workspace { grid-template-columns: 1fr; }
}

@media (max-width: 760px) {
  .system-certificates-workspace { flex: none; }
  .system-certificate-detail, .detail-body { overflow: visible; }
  .detail-header { align-items: stretch; flex-direction: column; }
  .detail-actions { justify-content: flex-start; }
  .detail-grid { grid-template-columns: 1fr; }
}
</style>
