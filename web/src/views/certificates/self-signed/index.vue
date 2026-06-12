<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { certificatesApi } from '@/api/certificates';
import { useI18n } from '@/i18n';
import type { SelfSignedCertificateDto } from '@/types/api';
import PageLoadingState from '@/components/PageLoadingState.vue';

const { t, formatDateTime } = useI18n();
const items = ref<SelfSignedCertificateDto[]>([]);
const loading = ref(false);
const busy = ref('');
const error = ref('');
const caDialog = ref(false);
const leafDialog = ref(false);
const deleteDialog = ref(false);
const deletingItem = ref<SelfSignedCertificateDto | null>(null);
const caForm = reactive({ name: '', commonName: '', years: 10 });
const leafForm = reactive({ name: '', caId: '', commonName: '', dnsNames: '', ipAddresses: '', days: 365 });
const cas = computed(() => items.value.filter((item) => item.kind === 'ca'));
const leaves = computed(() => items.value.filter((item) => item.kind === 'leaf'));
const caOptions = computed(() => cas.value.map((item) => ({ title: item.name, value: item.id })));

async function load() {
  loading.value = true;
  try {
    items.value = await certificatesApi.listSelfSigned();
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('selfSignedCertificates.loadFailed');
  } finally {
    loading.value = false;
  }
}

async function createCA() {
  busy.value = 'ca';
  try {
    await certificatesApi.createCA({ ...caForm });
    closeCADialog();
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('selfSignedCertificates.createFailed');
  } finally {
    busy.value = '';
  }
}

function openLeaf() {
  leafForm.caId = cas.value[0]?.id ?? '';
  leafDialog.value = true;
}

async function createLeaf() {
  busy.value = 'leaf';
  try {
    await certificatesApi.createSelfSigned({
      name: leafForm.name,
      caId: leafForm.caId,
      commonName: leafForm.commonName,
      dnsNames: splitValues(leafForm.dnsNames),
      ipAddresses: splitValues(leafForm.ipAddresses),
      days: leafForm.days,
    });
    closeLeafDialog();
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('selfSignedCertificates.createFailed');
  } finally {
    busy.value = '';
  }
}

async function renew(item: SelfSignedCertificateDto) {
  busy.value = `renew:${item.id}`;
  try {
    await certificatesApi.renewSelfSigned(item.id);
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('selfSignedCertificates.renewFailed');
  } finally {
    busy.value = '';
  }
}

function askRemove(item: SelfSignedCertificateDto) {
  deletingItem.value = item;
  deleteDialog.value = true;
}

async function removeSelected() {
  const item = deletingItem.value;
  if (!item) return;
  busy.value = `delete:${item.id}`;
  try {
    await certificatesApi.deleteSelfSigned(item.id);
    closeDeleteDialog();
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('selfSignedCertificates.deleteFailed');
  } finally {
    busy.value = '';
  }
}

function closeCADialog() {
  caDialog.value = false;
  Object.assign(caForm, { name: '', commonName: '', years: 10 });
}

function closeLeafDialog() {
  leafDialog.value = false;
  Object.assign(leafForm, { name: '', caId: '', commonName: '', dnsNames: '', ipAddresses: '', days: 365 });
}

function closeDeleteDialog() {
  deleteDialog.value = false;
  deletingItem.value = null;
}

function splitValues(value: string) {
  return value.split(/[\s,]+/).map((item) => item.trim()).filter(Boolean);
}

onMounted(load);
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>
    <PageLoadingState v-if="loading && items.length === 0" min-height="260px" />
    <div v-else class="self-signed-content">
      <v-card variant="outlined" class="self-signed-card">
        <div class="app-card-header">
          <div><div class="text-h6">{{ t('selfSignedCertificates.cas') }}</div><div class="text-body-2 text-medium-emphasis">{{ t('selfSignedCertificates.casHint') }}</div></div>
          <v-btn color="primary" variant="flat" prepend-icon="mdi-plus" class="text-none" @click="caDialog = true">{{ t('selfSignedCertificates.createCA') }}</v-btn>
        </div>
        <div class="table-scroll">
          <v-table density="comfortable">
            <thead><tr><th>{{ t('common.name') }}</th><th>{{ t('selfSignedCertificates.commonName') }}</th><th>{{ t('selfSignedCertificates.validUntil') }}</th><th class="text-right">{{ t('common.actions') }}</th></tr></thead>
            <tbody>
              <tr v-for="item in cas" :key="item.id">
                <td>{{ item.name }}</td><td>{{ item.commonName }}</td><td>{{ formatDateTime(item.notAfter) }}</td>
                <td class="text-right"><v-btn icon="mdi-delete" variant="text" color="error" :loading="busy === `delete:${item.id}`" @click="askRemove(item)" /></td>
              </tr>
            </tbody>
          </v-table>
        </div>
      </v-card>

      <v-card variant="outlined" class="self-signed-card">
        <div class="app-card-header">
          <div><div class="text-h6">{{ t('selfSignedCertificates.certificates') }}</div><div class="text-body-2 text-medium-emphasis">{{ t('selfSignedCertificates.certificatesHint') }}</div></div>
          <v-btn color="primary" variant="flat" prepend-icon="mdi-plus" class="text-none" :disabled="cas.length === 0" @click="openLeaf">{{ t('selfSignedCertificates.createCertificate') }}</v-btn>
        </div>
        <div class="table-scroll">
          <v-table density="comfortable">
            <thead><tr><th>{{ t('common.name') }}</th><th>{{ t('selfSignedCertificates.subjectAltNames') }}</th><th>{{ t('selfSignedCertificates.validUntil') }}</th><th class="text-right">{{ t('common.actions') }}</th></tr></thead>
            <tbody>
              <tr v-for="item in leaves" :key="item.id">
                <td>{{ item.name }}</td><td>{{ [...item.dnsNames, ...item.ipAddresses].join(', ') }}</td><td>{{ formatDateTime(item.notAfter) }}</td>
                <td class="text-right">
                  <v-btn icon="mdi-autorenew" variant="text" :loading="busy === `renew:${item.id}`" @click="renew(item)" />
                  <v-btn icon="mdi-delete" variant="text" color="error" :loading="busy === `delete:${item.id}`" @click="askRemove(item)" />
                </td>
              </tr>
            </tbody>
          </v-table>
        </div>
      </v-card>
    </div>

    <v-dialog v-model="caDialog" width="560">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('selfSignedCertificates.createCA') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="closeCADialog" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-text-field v-model="caForm.name" :label="t('common.name')" variant="outlined" density="comfortable" class="mb-3" />
          <v-text-field v-model="caForm.commonName" :label="t('selfSignedCertificates.commonName')" variant="outlined" density="comfortable" class="mb-3" />
          <v-text-field v-model.number="caForm.years" type="number" min="1" max="30" :label="t('selfSignedCertificates.validYears')" variant="outlined" density="comfortable" />
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="closeCADialog">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" variant="flat" class="text-none" :loading="busy === 'ca'" @click="createCA">{{ t('common.create') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
    <v-dialog v-model="leafDialog" width="620">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('selfSignedCertificates.createCertificate') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="closeLeafDialog" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-select v-model="leafForm.caId" :items="caOptions" :label="t('selfSignedCertificates.ca')" variant="outlined" density="comfortable" class="mb-3" />
          <v-text-field v-model="leafForm.name" :label="t('common.name')" variant="outlined" density="comfortable" class="mb-3" />
          <v-text-field v-model="leafForm.commonName" :label="t('selfSignedCertificates.commonName')" variant="outlined" density="comfortable" class="mb-3" />
          <v-textarea v-model="leafForm.dnsNames" :label="t('selfSignedCertificates.dnsNames')" rows="2" variant="outlined" density="comfortable" class="mb-3" />
          <v-textarea v-model="leafForm.ipAddresses" :label="t('selfSignedCertificates.ipAddresses')" rows="2" variant="outlined" density="comfortable" class="mb-3" />
          <v-text-field v-model.number="leafForm.days" type="number" min="1" max="3650" :label="t('selfSignedCertificates.validDays')" variant="outlined" density="comfortable" />
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="closeLeafDialog">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" variant="flat" class="text-none" :loading="busy === 'leaf'" @click="createLeaf">{{ t('common.create') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="deleteDialog" width="420">
      <v-card class="app-dialog-card">
        <v-card-title class="app-dialog-title">
          <span class="app-dialog-title-text">{{ t('selfSignedCertificates.deleteTitle') }}</span>
          <v-btn icon="mdi-close" variant="text" @click="closeDeleteDialog" />
        </v-card-title>
        <v-divider />
        <v-card-text class="app-dialog-body">
          <v-alert type="warning" variant="tonal" density="comfortable">
            {{ t('selfSignedCertificates.deleteConfirm', { name: deletingItem?.name || t('common.unknown') }) }}
          </v-alert>
        </v-card-text>
        <v-divider />
        <v-card-actions class="app-dialog-actions">
          <v-btn variant="text" class="text-none" @click="closeDeleteDialog">{{ t('common.cancel') }}</v-btn>
          <v-btn color="error" variant="flat" class="text-none" :loading="deletingItem ? busy === `delete:${deletingItem.id}` : false" @click="removeSelected">{{ t('common.delete') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </div>
</template>

<style scoped>
.self-signed-content {
  display: grid;
  grid-template-rows: minmax(0, 1fr) minmax(0, 1fr);
  gap: 16px;
  flex: 1 1 auto;
  min-height: 0;
}

.self-signed-card {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}

.table-scroll {
  min-height: 0;
  overflow: auto;
}

@media (max-width: 760px) {
  .self-signed-content {
    flex: none;
    grid-template-rows: none;
    min-height: auto;
  }

  .self-signed-card { overflow: visible; }
  .table-scroll { overflow-x: auto; overflow-y: visible; }
}
</style>
