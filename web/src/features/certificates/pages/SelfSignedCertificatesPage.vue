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
    caDialog.value = false;
    Object.assign(caForm, { name: '', commonName: '', years: 10 });
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
    leafDialog.value = false;
    Object.assign(leafForm, { name: '', caId: '', commonName: '', dnsNames: '', ipAddresses: '', days: 365 });
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

async function remove(item: SelfSignedCertificateDto) {
  if (!window.confirm(t('selfSignedCertificates.deleteConfirm', { name: item.name }))) return;
  busy.value = `delete:${item.id}`;
  try {
    await certificatesApi.deleteSelfSigned(item.id);
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('selfSignedCertificates.deleteFailed');
  } finally {
    busy.value = '';
  }
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
    <template v-else>
      <v-card variant="outlined" class="mb-4">
        <div class="app-card-header">
          <div><div class="text-h6">{{ t('selfSignedCertificates.cas') }}</div><div class="text-body-2 text-medium-emphasis">{{ t('selfSignedCertificates.casHint') }}</div></div>
          <v-btn color="primary" prepend-icon="mdi-plus" @click="caDialog = true">{{ t('selfSignedCertificates.createCA') }}</v-btn>
        </div>
        <v-table density="comfortable">
          <thead><tr><th>{{ t('common.name') }}</th><th>{{ t('selfSignedCertificates.commonName') }}</th><th>{{ t('selfSignedCertificates.validUntil') }}</th><th class="text-right">{{ t('common.actions') }}</th></tr></thead>
          <tbody>
            <tr v-for="item in cas" :key="item.id">
              <td>{{ item.name }}</td><td>{{ item.commonName }}</td><td>{{ formatDateTime(item.notAfter) }}</td>
              <td class="text-right"><v-btn icon="mdi-delete" variant="text" color="error" :loading="busy === `delete:${item.id}`" @click="remove(item)" /></td>
            </tr>
          </tbody>
        </v-table>
      </v-card>

      <v-card variant="outlined">
        <div class="app-card-header">
          <div><div class="text-h6">{{ t('selfSignedCertificates.certificates') }}</div><div class="text-body-2 text-medium-emphasis">{{ t('selfSignedCertificates.certificatesHint') }}</div></div>
          <v-btn color="primary" prepend-icon="mdi-plus" :disabled="cas.length === 0" @click="openLeaf">{{ t('selfSignedCertificates.createCertificate') }}</v-btn>
        </div>
        <v-table density="comfortable">
          <thead><tr><th>{{ t('common.name') }}</th><th>SAN</th><th>{{ t('selfSignedCertificates.validUntil') }}</th><th class="text-right">{{ t('common.actions') }}</th></tr></thead>
          <tbody>
            <tr v-for="item in leaves" :key="item.id">
              <td>{{ item.name }}</td><td>{{ [...item.dnsNames, ...item.ipAddresses].join(', ') }}</td><td>{{ formatDateTime(item.notAfter) }}</td>
              <td class="text-right">
                <v-btn icon="mdi-autorenew" variant="text" :loading="busy === `renew:${item.id}`" @click="renew(item)" />
                <v-btn icon="mdi-delete" variant="text" color="error" :loading="busy === `delete:${item.id}`" @click="remove(item)" />
              </td>
            </tr>
          </tbody>
        </v-table>
      </v-card>
    </template>

    <v-dialog v-model="caDialog" max-width="560">
      <v-card><v-card-title>{{ t('selfSignedCertificates.createCA') }}</v-card-title><v-card-text>
        <v-text-field v-model="caForm.name" :label="t('common.name')" />
        <v-text-field v-model="caForm.commonName" :label="t('selfSignedCertificates.commonName')" />
        <v-text-field v-model.number="caForm.years" type="number" min="1" max="30" :label="t('selfSignedCertificates.validYears')" />
      </v-card-text><v-card-actions><v-spacer /><v-btn @click="caDialog = false">{{ t('common.cancel') }}</v-btn><v-btn color="primary" :loading="busy === 'ca'" @click="createCA">{{ t('common.create') }}</v-btn></v-card-actions></v-card>
    </v-dialog>
    <v-dialog v-model="leafDialog" max-width="620">
      <v-card><v-card-title>{{ t('selfSignedCertificates.createCertificate') }}</v-card-title><v-card-text>
        <v-select v-model="leafForm.caId" :items="caOptions" :label="t('selfSignedCertificates.ca')" />
        <v-text-field v-model="leafForm.name" :label="t('common.name')" />
        <v-text-field v-model="leafForm.commonName" :label="t('selfSignedCertificates.commonName')" />
        <v-textarea v-model="leafForm.dnsNames" :label="t('selfSignedCertificates.dnsNames')" rows="2" />
        <v-textarea v-model="leafForm.ipAddresses" :label="t('selfSignedCertificates.ipAddresses')" rows="2" />
        <v-text-field v-model.number="leafForm.days" type="number" min="1" max="3650" :label="t('selfSignedCertificates.validDays')" />
      </v-card-text><v-card-actions><v-spacer /><v-btn @click="leafDialog = false">{{ t('common.cancel') }}</v-btn><v-btn color="primary" :loading="busy === 'leaf'" @click="createLeaf">{{ t('common.create') }}</v-btn></v-card-actions></v-card>
    </v-dialog>
  </div>
</template>
