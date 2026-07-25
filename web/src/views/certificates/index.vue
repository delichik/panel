<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { Download, FileArchive, KeyRound, Plus, RefreshCcw, RotateCcw, Trash2 } from '@lucide/vue';
import { certificatesApi } from '@/api/certificates';
import { dnsApi } from '@/api/dns';
import { keyAssetsApi } from '@/api/keyAssets';
import { saveBlobDownload } from '@/api/download';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import Dialog from '@/components/ui/Dialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Input from '@/components/ui/Input.vue';
import Select from '@/components/ui/Select.vue';
import Textarea from '@/components/ui/Textarea.vue';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import { useI18n } from '@/i18n';
import type { DomainCertificateDto, SelfSignedCertificateDto } from '@/types/certificates';
import type { DnsDomainDto } from '@/types/dns';
import type { ImportPreflightDto, KeyAssetDto, KeyAssetType } from '@/types/keyAssets';
import { assetTone, certificateState, certificateTone, selfSignedTone } from './model';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();

const domains = ref<DnsDomainDto[]>([]);
const certs = ref<DomainCertificateDto[]>([]);
const selfSigned = ref<SelfSignedCertificateDto[]>([]);
const assets = ref<KeyAssetDto[]>([]);
const selectedId = ref('');
const loading = ref(false);
const error = ref('');
const feedback = ref('');
const dialog = ref<'issue' | 'self-ca' | 'self-leaf' | 'asset-ca' | 'asset-tls' | 'asset-ssh' | 'asset-import' | 'asset-export' | 'asset-preflight' | ''>('');
const confirmDelete = ref(false);
const saving = ref(false);
const importPlan = ref<ImportPreflightDto | null>(null);

const mode = computed(() => route.path.includes('/self-signed') ? 'self' : route.path.includes('/keys') ? 'keys' : 'domains');
const title = computed(() => mode.value === 'self' ? t('routes.selfSigned.title') : mode.value === 'keys' ? t('routes.keys.title') : t('routes.certificates.title'));
const description = computed(() => mode.value === 'self' ? t('routes.selfSigned.description') : mode.value === 'keys' ? t('routes.keys.description') : t('routes.certificates.description'));
const selectedCert = computed(() => certs.value.find((item) => item.id === selectedId.value) ?? null);
const selectedSelf = computed(() => selfSigned.value.find((item) => item.id === selectedId.value) ?? null);
const selectedAsset = computed(() => assets.value.find((item) => item.id === selectedId.value) ?? null);
const caAssets = computed(() => assets.value.filter((item) => item.type === 'ca_certificate').map((item) => ({ label: item.name, value: item.id })));
const selfCas = computed(() => selfSigned.value.filter((item) => item.kind === 'ca').map((item) => ({ label: item.name, value: item.id })));
const domainOptions = computed(() => domains.value.map((item) => ({ label: item.name, value: item.id })));

const issueForm = reactive({ name: '', domainId: '', prefixes: '@', variableName: '' });
const selfForm = reactive({ name: '', caId: '', commonName: '', dnsNames: '', ipAddresses: '', years: '5', days: '90' });
const assetForm = reactive({ type: 'ssh_key_pair' as KeyAssetType, name: '', parentAssetId: '', commonName: '', dnsNames: '', ipAddresses: '', algorithm: 'ed25519', keySize: '2048', validityDays: '365', comment: '', certificatePem: '', privateKeyPem: '', publicKey: '', password: '' });
const importFile = ref<File | null>(null);

const algorithmOptions = [{ label: 'ed25519', value: 'ed25519' }, { label: 'rsa', value: 'rsa' }];
const assetTypeOptions = [
  { label: t('certificatesPage.assetCa'), value: 'ca_certificate' },
  { label: t('certificatesPage.assetTls'), value: 'tls_certificate' },
  { label: t('certificatesPage.assetSsh'), value: 'ssh_key_pair' },
];

onMounted(load);

async function load() {
  loading.value = true;
  error.value = '';
  try {
    const [nextDomains, nextCerts, nextSelf, nextAssets] = await Promise.all([
      dnsApi.listDomains(),
      certificatesApi.list(),
      certificatesApi.listSelfSigned(),
      keyAssetsApi.list(),
    ]);
    domains.value = nextDomains;
    certs.value = nextCerts;
    selfSigned.value = nextSelf;
    assets.value = nextAssets;
    const list = mode.value === 'self' ? nextSelf : mode.value === 'keys' ? nextAssets : nextCerts;
    selectedId.value = list.some((item) => item.id === selectedId.value) ? selectedId.value : list[0]?.id ?? '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('certificatesPage.loadFailed');
  } finally {
    loading.value = false;
  }
}

function switchMode(path: string) {
  selectedId.value = '';
  void router.push(path).then(load);
}

function openIssue() {
  Object.assign(issueForm, { name: '', domainId: domains.value[0]?.id ?? '', prefixes: '@', variableName: '' });
  dialog.value = 'issue';
}

async function issueCertificate() {
  saving.value = true;
  try {
    const prefixes = lines(issueForm.prefixes);
    const result = await certificatesApi.issue({ name: issueForm.name, domainId: issueForm.domainId, prefixes: prefixes.length ? prefixes : ['@'], variableName: issueForm.variableName });
    feedback.value = result.taskId ? t('certificatesPage.issueTask', { taskId: result.taskId }) : t('certificatesPage.issued');
    selectedId.value = result.certificate.id;
    dialog.value = '';
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.operationFailed');
  } finally {
    saving.value = false;
  }
}

async function renewCertificate(cert: DomainCertificateDto) {
  await run(async () => {
    await certificatesApi.renew(cert.id);
    feedback.value = t('certificatesPage.renewAccepted');
    await load();
  });
}

function openSelf(kind: 'self-ca' | 'self-leaf') {
  Object.assign(selfForm, { name: '', caId: selfCas.value[0]?.value ?? '', commonName: '', dnsNames: '', ipAddresses: '', years: '5', days: '90' });
  dialog.value = kind;
}

async function saveSelf() {
  saving.value = true;
  try {
    const saved = dialog.value === 'self-ca'
      ? await certificatesApi.createSelfSignedCa({ name: selfForm.name, commonName: selfForm.commonName, years: Number(selfForm.years) || 5 })
      : await certificatesApi.createSelfSignedLeaf({ name: selfForm.name, caId: selfForm.caId, commonName: selfForm.commonName, dnsNames: lines(selfForm.dnsNames), ipAddresses: lines(selfForm.ipAddresses), days: Number(selfForm.days) || 90 });
    selectedId.value = saved.id;
    feedback.value = t('certificatesPage.selfSaved');
    dialog.value = '';
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.operationFailed');
  } finally {
    saving.value = false;
  }
}

async function renewSelf(cert: SelfSignedCertificateDto) {
  await run(async () => {
    const renewed = await certificatesApi.renewSelfSigned(cert.id);
    selectedId.value = renewed.id;
    feedback.value = t('certificatesPage.selfRenewed');
    await load();
  });
}

function openAsset(next: typeof dialog.value) {
  Object.assign(assetForm, { type: 'ssh_key_pair', name: '', parentAssetId: caAssets.value[0]?.value ?? '', commonName: '', dnsNames: '', ipAddresses: '', algorithm: 'ed25519', keySize: '2048', validityDays: '365', comment: '', certificatePem: '', privateKeyPem: '', publicKey: '', password: '' });
  importPlan.value = null;
  importFile.value = null;
  dialog.value = next;
}

async function saveAsset() {
  saving.value = true;
  try {
    let result;
    if (dialog.value === 'asset-ca') result = await keyAssetsApi.createCa({ name: assetForm.name, commonName: assetForm.commonName, algorithm: assetForm.algorithm as 'ed25519' | 'rsa', keySize: Number(assetForm.keySize) || 0, years: 0, validityDays: Number(assetForm.validityDays) || 365 });
    if (dialog.value === 'asset-tls') result = await keyAssetsApi.createTls({ name: assetForm.name, parentAssetId: assetForm.parentAssetId, caId: assetForm.parentAssetId, commonName: assetForm.commonName, algorithm: assetForm.algorithm as 'ed25519' | 'rsa', keySize: Number(assetForm.keySize) || 0, dnsNames: lines(assetForm.dnsNames), ipAddresses: lines(assetForm.ipAddresses), days: 0, validityDays: Number(assetForm.validityDays) || 365 });
    if (dialog.value === 'asset-ssh') result = await keyAssetsApi.generateSsh({ name: assetForm.name, algorithm: assetForm.algorithm as 'ed25519' | 'rsa', keySize: Number(assetForm.keySize) || 0, comment: assetForm.comment });
    if (dialog.value === 'asset-import') result = await keyAssetsApi.importOne({ type: assetForm.type, name: assetForm.name, parentAssetId: assetForm.parentAssetId, commonName: assetForm.commonName, algorithm: assetForm.algorithm, keySize: Number(assetForm.keySize) || 0, certificatePem: assetForm.certificatePem, privateKeyPem: assetForm.privateKeyPem, publicKey: assetForm.publicKey });
    selectedId.value = result?.asset?.id ?? selectedId.value;
    feedback.value = t('certificatesPage.assetSaved');
    dialog.value = '';
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.operationFailed');
  } finally {
    saving.value = false;
  }
}

async function reissueAsset(asset: KeyAssetDto) {
  await run(async () => {
    const result = asset.type === 'ssh_key_pair' ? await keyAssetsApi.regenerate(asset.id) : await keyAssetsApi.reissue(asset.id);
    feedback.value = result.taskId ? t('certificatesPage.assetTask', { taskId: result.taskId }) : t('certificatesPage.assetSaved');
    await load();
  });
}

async function createExport() {
  await run(async () => {
    const result = await keyAssetsApi.createExport({ assetIds: selectedAsset.value ? [selectedAsset.value.id] : assets.value.map((item) => item.id), password: assetForm.password });
    feedback.value = t('certificatesPage.exportTask', { taskId: result.taskId });
    saveBlobDownload(await keyAssetsApi.downloadExport(result.taskId));
    dialog.value = '';
  });
}

async function downloadAssetFile(asset: KeyAssetDto, kind: string) {
  await run(async () => {
    saveBlobDownload(await keyAssetsApi.downloadFile(asset.id, kind));
  });
}

async function preflightImport() {
  if (!importFile.value) return;
  await run(async () => {
    importPlan.value = await keyAssetsApi.preflightImport(importFile.value as File, assetForm.password);
  });
}

async function executeImport(confirmDanger = false) {
  if (!importPlan.value) return;
  await run(async () => {
    const result = await keyAssetsApi.executeImport(importPlan.value!.planId, { strategy: 'overwrite', confirmOverwriteInUse: confirmDanger, confirmDangerousOverwrite: confirmDanger, resolutions: [] });
    feedback.value = t('certificatesPage.importTask', { taskId: result.taskId });
    dialog.value = '';
    await load();
  });
}

async function deleteSelected() {
  await run(async () => {
    if (mode.value === 'domains' && selectedCert.value) await certificatesApi.delete(selectedCert.value.id);
    if (mode.value === 'self' && selectedSelf.value) await certificatesApi.deleteSelfSigned(selectedSelf.value.id);
    if (mode.value === 'keys' && selectedAsset.value) await keyAssetsApi.delete(selectedAsset.value.id);
    feedback.value = t('certificatesPage.deleted');
    selectedId.value = '';
    confirmDelete.value = false;
    await load();
  });
}

async function run(action: () => Promise<void>) {
  saving.value = true;
  error.value = '';
  try {
    await action();
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('common.operationFailed');
  } finally {
    saving.value = false;
  }
}

function lines(raw: string) {
  return raw.split('\n').map((line) => line.trim()).filter(Boolean);
}

function onFile(event: Event) {
  importFile.value = ((event.target as HTMLInputElement).files ?? [])[0] ?? null;
}
</script>

<template>
  <ConsolePage :title="title" :description="description">
    <template #actions>
      <Button size="sm" :loading="loading" @click="load"><RefreshCcw />{{ t('common.refresh') }}</Button>
      <Button v-if="mode === 'domains'" size="sm" variant="primary" @click="openIssue"><Plus />{{ t('certificatesPage.issue') }}</Button>
      <Button v-else-if="mode === 'self'" size="sm" variant="primary" @click="openSelf('self-ca')"><Plus />{{ t('certificatesPage.generateCa') }}</Button>
      <Button v-else size="sm" variant="primary" @click="openAsset('asset-ssh')"><KeyRound />{{ t('certificatesPage.generateSsh') }}</Button>
    </template>

    <div class="grid h-full min-h-[640px] grid-rows-[auto_minmax(0,1fr)] gap-4">
      <nav class="flex flex-wrap gap-2 rounded-2xl border border-border bg-card p-2">
        <Button :variant="mode === 'domains' ? 'primary' : 'ghost'" @click="switchMode('/certificates/domains')">{{ t('routes.certificates.title') }}</Button>
        <Button :variant="mode === 'self' ? 'primary' : 'ghost'" @click="switchMode('/certificates/self-signed')">{{ t('routes.selfSigned.title') }}</Button>
        <Button :variant="mode === 'keys' ? 'primary' : 'ghost'" @click="switchMode('/certificates/keys')">{{ t('routes.keys.title') }}</Button>
      </nav>

      <div class="grid min-h-0 grid-cols-[360px_minmax(0,1fr)] gap-4 max-xl:grid-cols-1">
        <aside class="min-h-0 overflow-auto rounded-2xl border border-border bg-card p-2">
          <EmptyState v-if="mode === 'domains' && !certs.length" :title="t('certificatesPage.noCertificates')" :description="t('certificatesPage.noCertificatesHint')" />
          <button v-for="cert in mode === 'domains' ? certs : []" :key="cert.id" type="button" class="mb-2 grid w-full gap-2 rounded-xl border p-3 text-left hover:bg-accent" :class="selectedId === cert.id ? 'border-border-strong bg-background' : 'border-transparent'" @click="selectedId = cert.id">
            <div class="flex items-center justify-between gap-2"><strong class="truncate text-sm">{{ cert.name }}</strong><Badge :tone="certificateTone(cert)">{{ t(certificateState(cert)) }}</Badge></div>
            <span class="truncate text-xs text-muted-foreground">{{ cert.domains.join(', ') }}</span>
          </button>

          <EmptyState v-if="mode === 'self' && !selfSigned.length" :title="t('certificatesPage.noSelf')" :description="t('certificatesPage.noSelfHint')" />
          <button v-for="cert in mode === 'self' ? selfSigned : []" :key="cert.id" type="button" class="mb-2 grid w-full gap-2 rounded-xl border p-3 text-left hover:bg-accent" :class="selectedId === cert.id ? 'border-border-strong bg-background' : 'border-transparent'" @click="selectedId = cert.id">
            <div class="flex items-center justify-between gap-2"><strong class="truncate text-sm">{{ cert.name }}</strong><Badge :tone="selfSignedTone(cert)">{{ cert.kind }}</Badge></div>
            <span class="truncate text-xs text-muted-foreground">{{ cert.commonName }}</span>
          </button>

          <EmptyState v-if="mode === 'keys' && !assets.length" :title="t('certificatesPage.noAssets')" :description="t('certificatesPage.noAssetsHint')" />
          <button v-for="asset in mode === 'keys' ? assets : []" :key="asset.id" type="button" class="mb-2 grid w-full gap-2 rounded-xl border p-3 text-left hover:bg-accent" :class="selectedId === asset.id ? 'border-border-strong bg-background' : 'border-transparent'" @click="selectedId = asset.id">
            <div class="flex items-center justify-between gap-2"><strong class="truncate text-sm">{{ asset.name }}</strong><Badge :tone="assetTone(asset)">{{ asset.type }}</Badge></div>
            <span class="truncate text-xs text-muted-foreground">{{ asset.fingerprint || t('common.notAvailable') }}</span>
          </button>
        </aside>

        <main class="grid min-h-0 overflow-hidden rounded-2xl border border-border bg-card">
          <section v-if="error || feedback" class="border-b border-border p-4">
            <div v-if="error" class="rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">{{ error }}</div>
            <div v-if="feedback" class="rounded-xl border border-success-border bg-success-bg p-3 text-sm text-success">{{ feedback }}</div>
          </section>

          <EmptyState v-if="mode === 'domains' && !selectedCert" :title="t('certificatesPage.selectCertificate')" :description="t('certificatesPage.selectCertificateHint')" />
          <article v-else-if="mode === 'domains' && selectedCert" class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)]">
            <header class="flex items-start justify-between gap-3 border-b border-border p-5 max-md:grid">
              <div><h2 class="m-0 text-xl font-semibold">{{ selectedCert.name }}</h2><p class="m-0 mt-1 text-sm text-muted-foreground">{{ selectedCert.variableName }} / {{ selectedCert.issuer }}</p></div>
              <div class="flex flex-wrap gap-2">
                <Button size="sm" @click="renewCertificate(selectedCert)"><RotateCcw />{{ t('certificatesPage.renew') }}</Button>
                <Button size="sm" variant="danger" @click="confirmDelete = true"><Trash2 />{{ t('common.delete') }}</Button>
              </div>
            </header>
            <div class="min-h-0 overflow-auto p-5">
              <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
                <section class="rounded-2xl border border-border bg-background p-4">
                  <h3 class="m-0 text-sm font-semibold">{{ t('certificatesPage.coverage') }}</h3>
                  <div class="mt-3 flex flex-wrap gap-2"><Badge v-for="domain in selectedCert.domains" :key="domain" tone="info">{{ domain }}</Badge></div>
                  <p v-if="selectedCert.lastError" class="mt-4 rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">{{ selectedCert.lastError }}</p>
                </section>
                <aside class="grid content-start gap-3">
                  <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('common.status') }}</div><strong>{{ t(certificateState(selectedCert)) }}</strong></div>
                  <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.expiresAt') }}</div><strong>{{ selectedCert.notAfter || t('common.notAvailable') }}</strong></div>
                  <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.taskEntry') }}</div><strong>{{ selectedCert.status === 'issuing' ? t('certificatesPage.openTaskCenter') : t('certificatesPage.noActiveTask') }}</strong></div>
                </aside>
              </div>
            </div>
          </article>

          <EmptyState v-if="mode === 'self' && !selectedSelf" :title="t('certificatesPage.selectSelf')" :description="t('certificatesPage.selectSelfHint')" />
          <article v-else-if="mode === 'self' && selectedSelf" class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)]">
            <header class="flex items-start justify-between gap-3 border-b border-border p-5 max-md:grid">
              <div><h2 class="m-0 text-xl font-semibold">{{ selectedSelf.name }}</h2><p class="m-0 mt-1 text-sm text-muted-foreground">{{ selectedSelf.commonName }}</p></div>
              <div class="flex flex-wrap gap-2">
                <Button size="sm" @click="openSelf('self-leaf')"><Plus />{{ t('certificatesPage.generateLeaf') }}</Button>
                <Button size="sm" @click="renewSelf(selectedSelf)"><RotateCcw />{{ t('certificatesPage.reissue') }}</Button>
                <Button size="sm" variant="danger" @click="confirmDelete = true"><Trash2 />{{ t('common.delete') }}</Button>
              </div>
            </header>
            <div class="min-h-0 overflow-auto p-5">
              <div class="grid gap-3 md:grid-cols-2">
                <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('common.type') }}</div><strong>{{ selectedSelf.kind }}</strong></div>
                <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.fingerprint') }}</div><strong>{{ selectedSelf.fingerprint }}</strong></div>
                <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.expiresAt') }}</div><strong>{{ selectedSelf.notAfter }}</strong></div>
                <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">DNS</div><strong>{{ selectedSelf.dnsNames.join(', ') || t('common.notAvailable') }}</strong></div>
              </div>
            </div>
          </article>

          <EmptyState v-if="mode === 'keys' && !selectedAsset" :title="t('certificatesPage.selectAsset')" :description="t('certificatesPage.selectAssetHint')" />
          <article v-else-if="mode === 'keys' && selectedAsset" class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)]">
            <header class="flex items-start justify-between gap-3 border-b border-border p-5 max-md:grid">
              <div><h2 class="m-0 text-xl font-semibold">{{ selectedAsset.name }}</h2><p class="m-0 mt-1 text-sm text-muted-foreground">{{ selectedAsset.type }} / {{ selectedAsset.algorithm }}</p></div>
              <div class="flex flex-wrap gap-2">
                <Button size="sm" @click="openAsset('asset-ca')">{{ t('certificatesPage.generateCa') }}</Button>
                <Button size="sm" @click="openAsset('asset-tls')">{{ t('certificatesPage.generateTls') }}</Button>
                <Button size="sm" @click="openAsset('asset-import')">{{ t('certificatesPage.importAsset') }}</Button>
                <Button size="sm" @click="openAsset('asset-export')"><FileArchive />{{ t('certificatesPage.exportAsset') }}</Button>
                <Button size="sm" :disabled="!selectedAsset.canReissue && !selectedAsset.canRegenerate" @click="reissueAsset(selectedAsset)"><RotateCcw />{{ selectedAsset.type === 'ssh_key_pair' ? t('certificatesPage.regenerate') : t('certificatesPage.reissue') }}</Button>
                <Button size="sm" variant="danger" :disabled="!selectedAsset.canDelete" @click="confirmDelete = true"><Trash2 />{{ t('common.delete') }}</Button>
              </div>
            </header>
            <div class="min-h-0 overflow-auto p-5">
              <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
                <section class="grid gap-3 md:grid-cols-2">
                  <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.fingerprint') }}</div><strong>{{ selectedAsset.fingerprint }}</strong></div>
                  <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.references') }}</div><strong>{{ selectedAsset.referenceCount }}</strong></div>
                  <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.children') }}</div><strong>{{ selectedAsset.childCount }}</strong></div>
                  <div class="rounded-2xl border border-border bg-background p-4 text-sm"><div class="text-muted-foreground">{{ t('certificatesPage.expiresAt') }}</div><strong>{{ selectedAsset.notAfter || t('common.notAvailable') }}</strong></div>
                </section>
                <aside class="rounded-2xl border border-border bg-background p-4">
                  <h3 class="m-0 text-sm font-semibold">{{ t('certificatesPage.downloads') }}</h3>
                  <div class="mt-3 grid gap-2">
                    <Button v-for="kind in selectedAsset.downloadKinds" :key="kind" size="sm" @click="downloadAssetFile(selectedAsset, kind)"><Download />{{ kind }}</Button>
                  </div>
                </aside>
              </div>
            </div>
          </article>
        </main>
      </div>
    </div>

    <Dialog :open="dialog === 'issue'" :title="t('certificatesPage.issue')" :close-label="t('common.close')" @update:open="(open) => { if (!open) dialog = '' }">
      <div class="grid gap-3"><label class="grid gap-1 text-sm">{{ t('common.name') }}<Input v-model="issueForm.name" /></label><label class="grid gap-1 text-sm">{{ t('routes.dns.title') }}<Select v-model="issueForm.domainId" :options="domainOptions" /></label><label class="grid gap-1 text-sm">{{ t('certificatesPage.prefixes') }}<Textarea v-model="issueForm.prefixes" :placeholder="t('certificatesPage.prefixesPlaceholder')" /><span class="text-xs text-muted-foreground">{{ t('certificatesPage.prefixesHint') }}</span></label><label class="grid gap-1 text-sm">{{ t('certificatesPage.variableName') }}<Input v-model="issueForm.variableName" /></label></div>
      <template #footer><Button @click="dialog = ''">{{ t('common.cancel') }}</Button><Button variant="primary" :loading="saving" :disabled="!issueForm.name || !issueForm.domainId" @click="issueCertificate">{{ t('certificatesPage.issue') }}</Button></template>
    </Dialog>

    <Dialog :open="dialog === 'self-ca' || dialog === 'self-leaf'" :title="dialog === 'self-ca' ? t('certificatesPage.generateCa') : t('certificatesPage.generateLeaf')" :close-label="t('common.close')" @update:open="(open) => { if (!open) dialog = '' }">
      <div class="grid gap-3"><label class="grid gap-1 text-sm">{{ t('common.name') }}<Input v-model="selfForm.name" /></label><label v-if="dialog === 'self-leaf'" class="grid gap-1 text-sm">CA<Select v-model="selfForm.caId" :options="selfCas" /></label><label class="grid gap-1 text-sm">{{ t('certificatesPage.commonName') }}<Input v-model="selfForm.commonName" /></label><label v-if="dialog === 'self-leaf'" class="grid gap-1 text-sm">DNS<Textarea v-model="selfForm.dnsNames" /></label><label v-if="dialog === 'self-ca'" class="grid gap-1 text-sm">{{ t('certificatesPage.years') }}<Input v-model="selfForm.years" type="number" /></label><label v-else class="grid gap-1 text-sm">{{ t('certificatesPage.days') }}<Input v-model="selfForm.days" type="number" /></label></div>
      <template #footer><Button @click="dialog = ''">{{ t('common.cancel') }}</Button><Button variant="primary" :loading="saving" :disabled="!selfForm.name || !selfForm.commonName" @click="saveSelf">{{ t('common.create') }}</Button></template>
    </Dialog>

    <Dialog :open="dialog.startsWith('asset-') && !['asset-export','asset-preflight'].includes(dialog)" :title="t('certificatesPage.assetForm')" :close-label="t('common.close')" @update:open="(open) => { if (!open) dialog = '' }">
      <div class="grid gap-3">
        <label v-if="dialog === 'asset-import'" class="grid gap-1 text-sm">{{ t('common.type') }}<Select v-model="assetForm.type" :options="assetTypeOptions" /></label>
        <label class="grid gap-1 text-sm">{{ t('common.name') }}<Input v-model="assetForm.name" /></label>
        <label v-if="dialog === 'asset-tls' || (dialog === 'asset-import' && assetForm.type === 'tls_certificate')" class="grid gap-1 text-sm">CA<Select v-model="assetForm.parentAssetId" :options="caAssets" /></label>
        <label v-if="dialog !== 'asset-ssh'" class="grid gap-1 text-sm">{{ t('certificatesPage.commonName') }}<Input v-model="assetForm.commonName" /></label>
        <label class="grid gap-1 text-sm">{{ t('certificatesPage.algorithm') }}<Select v-model="assetForm.algorithm" :options="algorithmOptions" /></label>
        <label v-if="assetForm.algorithm === 'rsa'" class="grid gap-1 text-sm">{{ t('certificatesPage.keySize') }}<Input v-model="assetForm.keySize" type="number" /></label>
        <label v-if="dialog === 'asset-tls'" class="grid gap-1 text-sm">DNS<Textarea v-model="assetForm.dnsNames" /></label>
        <label v-if="dialog === 'asset-import'" class="grid gap-1 text-sm">{{ t('certificatesPage.privateKeyPem') }}<Textarea v-model="assetForm.privateKeyPem" /></label>
        <label v-if="dialog === 'asset-import' && assetForm.type !== 'ssh_key_pair'" class="grid gap-1 text-sm">{{ t('certificatesPage.certificatePem') }}<Textarea v-model="assetForm.certificatePem" /></label>
      </div>
      <template #footer><Button @click="dialog = ''">{{ t('common.cancel') }}</Button><Button variant="primary" :loading="saving" :disabled="!assetForm.name" @click="saveAsset">{{ t('common.save') }}</Button></template>
    </Dialog>

    <Dialog :open="dialog === 'asset-export'" :title="t('certificatesPage.exportAsset')" :close-label="t('common.close')" @update:open="(open) => { if (!open) dialog = '' }">
      <div class="grid gap-3"><p class="m-0 text-sm text-muted-foreground">{{ t('certificatesPage.exportHint') }}</p><label class="grid gap-1 text-sm">{{ t('certificatesPage.archivePassword') }}<Input v-model="assetForm.password" type="password" /></label><label class="grid gap-1 text-sm">{{ t('certificatesPage.importArchive') }}<input type="file" class="text-sm" @change="onFile" /></label><Button :disabled="!importFile || !assetForm.password" @click="preflightImport">{{ t('certificatesPage.preflightImport') }}</Button></div>
      <div v-if="importPlan" class="mt-4 rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ t('certificatesPage.importConflicts', { count: importPlan.summary.conflictCount }) }}</div>
      <template #footer><Button @click="dialog = ''">{{ t('common.cancel') }}</Button><Button :loading="saving" :disabled="!assetForm.password" @click="createExport">{{ t('certificatesPage.exportAsset') }}</Button><Button v-if="importPlan" variant="danger" :loading="saving" @click="executeImport(true)">{{ t('certificatesPage.executeImport') }}</Button></template>
    </Dialog>

    <Dialog v-model:open="confirmDelete" :title="t('certificatesPage.deleteTitle')" :close-label="t('common.close')">
      <p class="m-0 text-sm text-muted-foreground">{{ t('certificatesPage.deleteHint') }}</p>
      <template #footer><Button @click="confirmDelete = false">{{ t('common.cancel') }}</Button><Button variant="danger" :loading="saving" @click="deleteSelected">{{ t('common.delete') }}</Button></template>
    </Dialog>
  </ConsolePage>
</template>
