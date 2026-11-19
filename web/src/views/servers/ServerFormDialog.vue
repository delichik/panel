<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue';
import { ChevronDown, Cable } from '@/icons';
import { credentialsApi } from '@/api/credentials';
import { serversApi } from '@/api/servers';
import Button from '@/components/ui/Button.vue';
import Dialog from '@/components/ui/Dialog.vue';
import Input from '@/components/ui/Input.vue';
import NumberBox from '@/components/ui/NumberBox.vue';
import Select from '@/components/ui/Select.vue';
import Textarea from '@/components/ui/Textarea.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import CredentialFormFields from '@/components/patterns/CredentialFormFields.vue';
import FormField from '@/components/patterns/FormField.vue';
import { emptyCredentialInput, secretPayload, validateCredentialInput, type CredentialFormLabels } from '@/components/patterns/credentialForm';
import { useI18n } from '@/i18n';
import type { CredentialDto, CredentialInput } from '@/types/credentials';
import type { ServerDto, ServerProbeInput, ServerProbeResult, ServerProxyMode, ServerSaveInput } from '@/types/servers';
import {
  connectionSignature,
  hasBlockingPairIssues,
  mergeServerCandidates,
  parsePairs,
  proxySavePayload,
  stringifyPairs,
  validateProbeInput,
  validateServerInput,
} from './model';

/**
 * 服务器创建/编辑弹窗。探测是独立的连接诊断：只要求地址、端口、凭据与代理草稿有效，
 * 结果（含过期）不参与创建闸门；创建闸门只看完整表单校验。
 */
const props = defineProps<{
  open: boolean;
  editing: ServerDto | null;
  credentials: CredentialDto[];
  credentialError: string;
  /** 隧道服务器候选；由列表页传入，弹窗内排除当前编辑服务器自身。 */
  servers: ServerDto[];
}>();

const emit = defineEmits<{
  'update:open': [value: boolean];
  saved: [value: ServerDto];
  'refresh-credentials': [];
}>();

const { t } = useI18n();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const form = reactive({
  name: '',
  ipv4: '',
  ipv6: '',
  port: '22',
  sshUsername: '',
  credentialId: '',
  dockerHost: 'unix:///var/run/docker.sock',
  proxyMode: 'direct' as ServerProxyMode,
  tunnelServerId: '',
  socks5Address: '',
  socks5Username: '',
  socks5Password: '',
  variables: '',
  notes: '',
});
const actionError = ref('');
const saving = ref(false);
const probing = ref(false);
const probeResult = ref<ServerProbeResult | null>(null);
const probeSignature = ref('');
const advancedOpen = ref(false);
const credentialDialogOpen = ref(false);
const credentialForm = ref<CredentialInput>(emptyCredentialInput());
const credentialSaving = ref(false);
const credentialActionError = ref('');
// 隧道候选：弹窗打开时拉取全量服务器（serversApi.list 固定 pageSize=200），
// 与父页面分页列表合并去重；加载失败只降级提示，不阻塞直连模式保存。
const allServers = ref<ServerDto[]>([]);
const candidatesLoading = ref(false);
const candidatesError = ref('');
let candidatesRequestId = 0;
// 隧道服务器搜索：打开下拉后按名称/地址远程搜索，覆盖 200 台以外无法预载的
// 服务器；搜索结果合并进候选集，输入清空或弹窗关闭时丢弃。
const remoteServers = ref<ServerDto[]>([]);
const tunnelSearchError = ref('');
let tunnelSearchRequestId = 0;
let tunnelSearchTimer: number | undefined;
const tunnelSearchDebounceMs = 250;

const credentialOptions = computed(() => props.credentials.map((item) => ({ label: `${item.name} / ${item.username}`, value: item.id })));
const proxyModeOptions = computed(() => [
  { value: 'direct', label: t('serversPage.proxy.modeDirect') },
  { value: 'ssh_tunnel', label: t('serversPage.proxy.modeSshTunnel') },
  { value: 'socks5', label: t('serversPage.proxy.modeSocks5') },
]);
// 隧道服务器候选：分页列表、全量与远程搜索结果按 id 合并去重，并排除当前
// 编辑服务器自身；当前值不在候选集中时保留一个占位选项，避免编辑态选择丢失。
const tunnelServerOptions = computed(() => {
  const options = mergeServerCandidates(props.servers, [...allServers.value, ...remoteServers.value], props.editing?.id)
    .map((server) => ({ value: server.id, label: `${server.name} · ${server.host}` }));
  const selectedId = form.tunnelServerId.trim();
  if (selectedId && !options.some((option) => option.value === selectedId)) {
    const knownName = props.editing?.tunnelServerId === selectedId ? props.editing.tunnelServerName : '';
    options.unshift({ value: selectedId, label: `${knownName || selectedId} · ${t('serversPage.proxy.tunnelUnknown')}` });
  }
  return options;
});
const socks5PasswordStored = computed(() => Boolean(props.editing?.socks5PasswordSet));
const proxyPayload = computed(() => proxySavePayload({
  proxyMode: form.proxyMode,
  tunnelServerId: form.tunnelServerId,
  socks5Address: form.socks5Address,
  socks5Username: form.socks5Username,
  socks5Password: form.socks5Password,
}));
const formPayload = computed<ServerSaveInput>(() => ({
  name: form.name,
  ipv4: form.ipv4,
  ipv6: form.ipv6,
  port: Number(form.port),
  sshUsername: form.sshUsername,
  credentialId: form.credentialId,
  dockerHost: form.dockerHost,
  ...proxyPayload.value,
  variables: parsePairs(form.variables).pairs,
  notes: form.notes,
}));
// 编辑探测时附带 serverId：密码留空可复用已存密文，并把待保存服务器纳入成环检测。
const probePayload = computed<ServerProbeInput>(() => ({
  ipv4: form.ipv4,
  ipv6: form.ipv6,
  port: Number(form.port),
  sshUsername: form.sshUsername,
  credentialId: form.credentialId,
  ...proxyPayload.value,
  ...(props.editing ? { serverId: props.editing.id } : {}),
}));
const validation = computed(() => validateServerInput(formPayload.value, { selfId: props.editing?.id }));
const probeValidation = computed(() => validateProbeInput(probePayload.value));

/** NumberBox 使用 number|null；草稿/保存负载仍是 string，保持既有校验与焦点摘要行为。 */
const portModelValue = computed<number | null>(() => {
  const trimmed = form.port.trim();
  if (!trimmed) return null;
  const parsed = Number(trimmed);
  return Number.isFinite(parsed) ? parsed : null;
});

function updatePort(value: number | null) {
  form.port = value === null ? '' : String(value);
}
const pairResult = computed(() => parsePairs(form.variables));
const pairErrors = computed(() => pairResult.value.issues.filter((issue) => issue.kind !== 'duplicate_key'));
const pairWarnings = computed(() => pairResult.value.issues.filter((issue) => issue.kind === 'duplicate_key'));
const canSave = computed(() => !Object.keys(validation.value).length && !hasBlockingPairIssues(pairResult.value.issues));
const probeStale = computed(() => Boolean(probeResult.value) && probeSignature.value !== connectionSignature(formPayload.value));
const probeToneClass = computed(() => {
  if (probeStale.value) return 'border-warning-border bg-warning-bg text-warning';
  return probeResult.value?.reachable ? 'border-success-border bg-success-bg text-success' : 'border-danger-border bg-danger-bg text-danger';
});
const probeSummary = computed(() => {
  const result = probeResult.value;
  if (!result?.reachable) return '';
  const privilege = result.root ? t('serversPage.privileged') : result.passwordlessSudo ? t('serversPage.passwordlessSudo') : t('serversPage.noPrivilege');
  return [result.os?.prettyName, result.architecture?.arch, privilege].filter(Boolean).join(' · ');
});
const summaryItems = computed(() => {
  const items = Object.entries(validation.value).map(([field, key]) => ({ field, text: t(key) }));
  for (const issue of pairErrors.value) {
    items.push({ field: 'variables', text: t(issue.kind === 'missing_separator' ? 'serversPage.variablesErrorMissingSeparator' : 'serversPage.variablesErrorEmptyKey', { line: issue.line }) });
  }
  return items;
});
const credentialValidation = computed(() => validateCredentialInput(credentialForm.value, false));
const credentialFormLabels = computed<CredentialFormLabels>(() => ({
  name: t('credentialsPage.name'),
  type: t('credentialsPage.type'),
  username: t('credentialsPage.username'),
  password: t('credentialsPage.password'),
  privateKey: t('credentialsPage.privateKey'),
  passphrase: t('credentialsPage.passphrase'),
  leaveSecretBlank: t('credentialsPage.leaveSecretBlank'),
  typeChangedRequiresSecret: t('credentialsPage.typeChangedRequiresSecret'),
  blankSecretKeepsCurrent: t('credentialsPage.blankSecretKeepsCurrent'),
}));
const credentialTypeOptions = computed(() => [
  { value: 'password', label: t('credentialsPage.password') },
  { value: 'private_key', label: t('credentialsPage.privateKey') },
]);

async function loadTunnelCandidates() {
  const requestId = ++candidatesRequestId;
  candidatesLoading.value = true;
  candidatesError.value = '';
  try {
    const list = await serversApi.list();
    if (requestId !== candidatesRequestId) return;
    allServers.value = list;
  } catch {
    if (requestId !== candidatesRequestId) return;
    // 失败不阻塞表单：回退到父页面已加载的分页列表，仅在隧道选择处提示可重试。
    allServers.value = [];
    candidatesError.value = t('serversPage.proxy.candidatesLoadFailed');
  } finally {
    if (requestId === candidatesRequestId) candidatesLoading.value = false;
  }
}

/** 隧道服务器搜索：防抖后按名称/地址查询，结果合并到候选集。 */
function onTunnelSearch(query: string) {
  window.clearTimeout(tunnelSearchTimer);
  tunnelSearchError.value = '';
  const term = query.trim();
  if (!term) {
    tunnelSearchRequestId += 1;
    remoteServers.value = [];
    return;
  }
  tunnelSearchTimer = window.setTimeout(() => {
    void searchTunnelServers(term);
  }, tunnelSearchDebounceMs);
}

async function searchTunnelServers(term: string) {
  const requestId = ++tunnelSearchRequestId;
  try {
    const result = await serversApi.listPage({ page: 1, pageSize: 50, q: term });
    if (requestId !== tunnelSearchRequestId) return;
    const merged = new Map(remoteServers.value.map((server) => [server.id, server]));
    for (const item of result.items) merged.set(item.id, item);
    remoteServers.value = [...merged.values()];
  } catch {
    if (requestId !== tunnelSearchRequestId) return;
    tunnelSearchError.value = t('serversPage.proxy.searchTunnelFailed');
  }
}

/** 关闭隧道选择/关闭弹窗时丢弃远程搜索状态与在途请求。 */
function resetTunnelSearch() {
  window.clearTimeout(tunnelSearchTimer);
  tunnelSearchRequestId += 1;
  remoteServers.value = [];
  tunnelSearchError.value = '';
}

watch(() => props.open, (open) => {
  if (!open) {
    candidatesRequestId += 1;
    candidatesLoading.value = false;
    resetTunnelSearch();
    return;
  }
  allServers.value = [];
  candidatesError.value = '';
  resetTunnelSearch();
  void loadTunnelCandidates();
  actionError.value = '';
  probeResult.value = null;
  probeSignature.value = '';
  credentialDialogOpen.value = false;
  advancedOpen.value = Boolean(props.editing);
  if (props.editing) {
    Object.assign(form, {
      name: props.editing.name,
      ipv4: props.editing.ipv4 ?? '',
      ipv6: props.editing.ipv6 ?? '',
      port: String(props.editing.port || 22),
      sshUsername: props.editing.sshUsername ?? '',
      credentialId: props.editing.credentialId,
      dockerHost: props.editing.dockerHost || 'unix:///var/run/docker.sock',
      proxyMode: props.editing.proxyMode ?? 'direct',
      tunnelServerId: props.editing.tunnelServerId ?? '',
      socks5Address: props.editing.socks5Address ?? '',
      socks5Username: props.editing.socks5Username ?? '',
      socks5Password: '',
      variables: stringifyPairs(props.editing.variables),
      notes: props.editing.notes ?? '',
    });
  } else {
    Object.assign(form, {
      name: '',
      ipv4: '',
      ipv6: '',
      port: '22',
      sshUsername: '',
      credentialId: props.credentials[0]?.id ?? '',
      dockerHost: 'unix:///var/run/docker.sock',
      proxyMode: 'direct',
      tunnelServerId: '',
      socks5Address: '',
      socks5Username: '',
      socks5Password: '',
      variables: '',
      notes: '',
    });
  }
});

// 弹窗先于凭据列表打开时，列表到达后补默认选中
watch(() => props.credentials, (list) => {
  if (props.open && !props.editing && !form.credentialId && list.length) form.credentialId = list[0].id;
});

watch(credentialDialogOpen, (open) => {
  if (open) {
    credentialForm.value = emptyCredentialInput();
    credentialActionError.value = '';
  }
});

function close() {
  emit('update:open', false);
}

const advancedFieldIds: Record<string, string> = {
  dockerHost: 'server-form-docker-host',
  variables: 'server-form-variables',
  notes: 'server-form-notes',
};

const proxyFieldIds: Record<string, string> = {
  tunnelServerId: 'server-form-tunnel-server',
  socks5Address: 'server-form-socks5-address',
  socks5Username: 'server-form-socks5-username',
};

async function focusField(field: string) {
  if (field in advancedFieldIds) advancedOpen.value = true;
  await nextTick();
  const selector = field === 'credentialId'
    ? '#server-form-credential'
    : `#${advancedFieldIds[field] ?? proxyFieldIds[field] ?? `server-form-${field}`}`;
  document.querySelector<HTMLElement>(selector)?.focus();
}

async function probe() {
  if (Object.keys(probeValidation.value).length) return;
  probing.value = true;
  probeResult.value = null;
  actionError.value = '';
  try {
    probeResult.value = await serversApi.probe(probePayload.value);
    probeSignature.value = connectionSignature(probePayload.value);
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('serversPage.probeFailed');
    notifyError(err instanceof Error ? err.message : t('serversPage.probeFailed'), err);
  } finally {
    probing.value = false;
  }
}

async function save() {
  if (!canSave.value) return;
  saving.value = true;
  actionError.value = '';
  try {
    const saved = props.editing ? await serversApi.update(props.editing.id, formPayload.value) : await serversApi.create(formPayload.value);
    notifySuccess(t(props.editing ? 'serversPage.updated' : saved.initialTaskId ? 'serversPage.createdInitializing' : 'serversPage.created'), saved);
    close();
    emit('saved', saved);
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('serversPage.saveFailed');
    notifyError(err instanceof Error ? err.message : t('serversPage.saveFailed'), err);
  } finally {
    saving.value = false;
  }
}

async function saveQuickCredential() {
  if (Object.keys(credentialValidation.value).length) return;
  credentialSaving.value = true;
  credentialActionError.value = '';
  try {
    const saved = await credentialsApi.create(secretPayload(credentialForm.value, false));
    notifySuccess(t('credentialsPage.created'), saved);
    credentialDialogOpen.value = false;
    form.credentialId = saved.id;
    emit('refresh-credentials');
  } catch (err) {
    credentialActionError.value = err instanceof Error ? err.message : t('credentialsPage.saveFailed');
    notifyError(err instanceof Error ? err.message : t('credentialsPage.saveFailed'), err);
  } finally {
    credentialSaving.value = false;
  }
}
</script>

<template>
  <Dialog :open="open" :title="editing ? t('serversPage.editServer') : t('serversPage.createServer')" :description="t(editing ? 'serversPage.editServerDescription' : 'serversPage.formDescription')" :close-label="t('common.close')" @update:open="emit('update:open', $event)">
    <div class="grid gap-4">
      <section class="grid gap-3">
        <h3 class="m-0 text-body-strong font-semibold text-foreground">{{ t('serversPage.sectionConnection') }}</h3>
        <div class="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
          <FormField id="server-form-name" v-slot="{ id, describedBy, invalid }" :label="t('serversPage.name')" :error="validation.name ? t(validation.name) : ''">
            <Input :id="id" v-model="form.name" :aria-describedby="describedBy" :invalid="invalid" />
          </FormField>
          <FormField id="server-form-port" v-slot="{ id, describedBy, invalid }" :label="t('serversPage.port')" :error="validation.port ? t(validation.port) : ''">
            <NumberBox
              :id="id"
              :model-value="portModelValue"
              :aria-describedby="describedBy"
              :invalid="invalid"
              :min="1"
              :max="65535"
              :step="1"
              @update:model-value="updatePort"
            />
          </FormField>
          <FormField id="server-form-ipv4" v-slot="{ id, describedBy, invalid }" :label="t('serversPage.ipv4')" :error="validation.ipv4 ? t(validation.ipv4) : ''">
            <Input :id="id" v-model="form.ipv4" :aria-describedby="describedBy" :invalid="invalid" placeholder="203.0.113.10" />
          </FormField>
          <FormField id="server-form-ipv6" v-slot="{ id, describedBy, invalid }" :label="t('serversPage.ipv6')" :error="validation.ipv6 ? t(validation.ipv6) : ''">
            <Input :id="id" v-model="form.ipv6" :aria-describedby="describedBy" :invalid="invalid" placeholder="2001:db8::10" />
          </FormField>
        </div>
        <p class="m-0 text-caption text-muted-foreground">{{ t('serversPage.addressHint') }} <span class="panel-mono">{{ form.ipv4.trim() || form.ipv6.trim() || t('common.notAvailable') }}</span></p>

        <template v-if="credentialError">
          <div class="grid gap-2 rounded-control border border-danger-border bg-danger-bg p-3 text-body text-danger">
            <span>{{ credentialError }}</span>
            <span><Button size="sm" variant="secondary" @click="emit('refresh-credentials')">{{ t('common.retry') }}</Button></span>
          </div>
        </template>
        <template v-else-if="credentialOptions.length">
          <FormField id="server-form-credential" v-slot="{ id, describedBy, invalid }" :label="t('serversPage.credential')" :error="validation.credentialId ? t(validation.credentialId) : ''">
            <Select :id="id" v-model="form.credentialId" :aria-describedby="describedBy" :aria-invalid="invalid || undefined" :options="credentialOptions" :placeholder="t('serversPage.selectCredential')" />
          </FormField>
          <FormField v-slot="{ id, describedBy }" :label="t('serversPage.sshUsername')">
            <Input :id="id" v-model="form.sshUsername" :aria-describedby="describedBy" :placeholder="t('serversPage.sshUsernameHint')" />
          </FormField>
        </template>
        <template v-else>
          <div class="grid gap-2 rounded-control border border-divider bg-muted p-3">
            <strong class="text-body font-semibold text-foreground">{{ t('serversPage.credentialMissingTitle') }}</strong>
            <p class="m-0 text-body text-muted-foreground">{{ t('serversPage.credentialMissingHint') }}</p>
            <span><Button size="sm" variant="primary" @click="credentialDialogOpen = true">{{ t('credentialsPage.addCredential') }}</Button></span>
          </div>
        </template>

        <div class="grid gap-2 rounded-control border border-divider bg-muted p-3">
          <h4 class="m-0 text-caption font-semibold text-foreground">{{ t('serversPage.proxy.title') }}</h4>
          <FormField id="server-form-proxy-mode" v-slot="{ id, describedBy }" :label="t('serversPage.proxy.mode')">
            <Select :id="id" v-model="form.proxyMode" :aria-describedby="describedBy" :options="proxyModeOptions" />
          </FormField>
          <template v-if="form.proxyMode === 'ssh_tunnel'">
            <FormField
              id="server-form-tunnel-server"
              v-slot="{ id, describedBy, invalid }"
              :label="t('serversPage.proxy.tunnelServer')"
              :hint="t('serversPage.proxy.tunnelHint')"
              :error="validation.tunnelServerId ? t(validation.tunnelServerId) : ''"
            >
              <Select :id="id" v-model="form.tunnelServerId" :aria-describedby="describedBy" :aria-invalid="invalid || undefined" :options="tunnelServerOptions" :placeholder="t('serversPage.proxy.selectTunnelServer')" searchable :search-placeholder="t('serversPage.proxy.searchTunnelPlaceholder')" :search-empty-text="t('serversPage.proxy.searchTunnelEmpty')" @search="onTunnelSearch" />
            </FormField>
            <p v-if="tunnelSearchError" class="m-0 text-caption text-warning">{{ tunnelSearchError }}</p>
            <p v-if="candidatesLoading" class="m-0 text-caption text-muted-foreground">{{ t('serversPage.proxy.candidatesLoading') }}</p>
            <p v-else-if="candidatesError" class="m-0 flex flex-wrap items-center gap-2 text-caption text-warning">
              <span>{{ candidatesError }}</span>
              <Button size="sm" variant="secondary" @click="loadTunnelCandidates">{{ t('common.retry') }}</Button>
            </p>
            <p v-if="!tunnelServerOptions.length && !candidatesLoading && !candidatesError" class="m-0 text-caption text-warning">{{ t('serversPage.proxy.noTunnelCandidates') }}</p>
          </template>
          <template v-else-if="form.proxyMode === 'socks5'">
            <div class="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
              <FormField id="server-form-socks5-address" v-slot="{ id, describedBy, invalid }" :label="t('serversPage.proxy.socks5Address')" :error="validation.socks5Address ? t(validation.socks5Address) : ''">
                <Input :id="id" v-model="form.socks5Address" :aria-describedby="describedBy" :invalid="invalid" :placeholder="t('serversPage.proxy.socks5AddressPlaceholder')" />
              </FormField>
              <FormField id="server-form-socks5-username" v-slot="{ id, describedBy, invalid }" :label="t('serversPage.proxy.socks5Username')" :error="validation.socks5Username ? t(validation.socks5Username) : ''">
                <Input :id="id" v-model="form.socks5Username" :aria-describedby="describedBy" :invalid="invalid" />
              </FormField>
            </div>
            <FormField id="server-form-socks5-password" v-slot="{ id, describedBy }" :label="t('serversPage.proxy.socks5Password')" :hint="t('serversPage.proxy.socks5AuthHint')">
              <Input :id="id" v-model="form.socks5Password" :aria-describedby="describedBy" type="password" autocomplete="new-password" :placeholder="socks5PasswordStored ? t('serversPage.proxy.socks5PasswordKeep') : ''" />
            </FormField>
          </template>
          <p v-else class="m-0 text-caption text-muted-foreground">{{ t('serversPage.proxy.directHint') }}</p>
        </div>

        <div class="grid gap-2 rounded-control border border-divider bg-muted p-3">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <p class="m-0 text-caption text-muted-foreground">{{ t('serversPage.probeHint') }}</p>
            <Button size="sm" variant="secondary" :disabled="Boolean(Object.keys(probeValidation).length)" :loading="probing" @click="probe"><Cable />{{ probeResult ? t('serversPage.probeAgain') : t('serversPage.probe') }}</Button>
          </div>
          <div v-if="probeResult" class="rounded-control border p-3 text-body" :class="probeToneClass">
            <p class="m-0">{{ t(probeResult.reachable ? 'serversPage.probeReachable' : 'serversPage.probeUnreachable') }}<span v-if="probeResult.error"> {{ probeResult.error }}</span></p>
            <p v-if="probeSummary" class="m-0 mt-1 text-caption">{{ probeSummary }}</p>
            <p v-if="probeStale" class="m-0 mt-1 text-caption font-medium">{{ t('serversPage.probeStale') }}</p>
          </div>
        </div>
      </section>

      <div v-if="!editing" class="rounded-control border border-warning-border bg-warning-bg p-3 text-body text-warning">
        {{ t('serversPage.firewallTakeoverNotice') }}
      </div>

      <section class="grid gap-3 border-t border-divider pt-3">
        <button type="button" class="flex w-full items-center justify-between gap-2 rounded-control px-1 py-1 text-left text-body-strong font-semibold text-foreground hover:bg-hover" :aria-expanded="advancedOpen" @click="advancedOpen = !advancedOpen">
          {{ t('serversPage.sectionAdvanced') }}
          <ChevronDown class="size-4 text-muted-foreground transition-transform" :class="advancedOpen ? 'rotate-180' : ''" />
        </button>
        <div v-show="advancedOpen" class="grid gap-3">
          <FormField
            id="server-form-docker-host"
            v-slot="{ id, describedBy, invalid }"
            :label="t('serversPage.dockerHost')"
            :hint="t('serversPage.dockerHostHint')"
            :error="validation.dockerHost ? t(validation.dockerHost) : ''"
          >
            <Input :id="id" v-model="form.dockerHost" :aria-describedby="describedBy" :invalid="invalid" />
          </FormField>
          <div class="grid gap-1">
            <FormField id="server-form-variables" v-slot="{ id, describedBy }" :label="t('serversPage.variables')">
              <Textarea :id="id" v-model="form.variables" :aria-describedby="describedBy" :placeholder="t('serversPage.pairsHint')" class="font-mono" />
            </FormField>
            <p v-for="issue in pairErrors" :key="`e${issue.line}`" class="m-0 text-caption text-danger">{{ t(issue.kind === 'missing_separator' ? 'serversPage.variablesErrorMissingSeparator' : 'serversPage.variablesErrorEmptyKey', { line: issue.line }) }}</p>
            <p v-for="issue in pairWarnings" :key="`w${issue.line}`" class="m-0 text-caption text-warning">{{ t('serversPage.variablesWarningDuplicateKey', { line: issue.line, key: issue.key ?? '' }) }}</p>
          </div>
          <FormField id="server-form-notes" v-slot="{ id, describedBy }" :label="t('serversPage.notes')">
            <Textarea :id="id" v-model="form.notes" :aria-describedby="describedBy" />
          </FormField>
        </div>
      </section>

      <div v-if="summaryItems.length" class="grid gap-1 rounded-control border border-warning-border bg-warning-bg p-3 text-body text-warning">
        <button v-for="item in summaryItems" :key="`${item.field}-${item.text}`" type="button" class="text-left underline-offset-2 hover:underline" @click="focusField(item.field)">{{ item.text }}</button>
      </div>
      <div v-if="actionError" class="rounded-control border border-danger-border bg-danger-bg p-3 text-body text-danger">{{ actionError }}</div>
    </div>
    <template #footer>
      <Button variant="secondary" @click="close">{{ t('common.cancel') }}</Button>
      <Button variant="primary" :loading="saving" :disabled="!canSave" @click="save">{{ editing ? t('common.save') : t('common.create') }}</Button>
    </template>
  </Dialog>

  <Dialog v-model:open="credentialDialogOpen" :size="credentialForm.type === 'private_key' ? 'large' : 'default'" :title="t('credentialsPage.createCredential')" :description="t('credentialsPage.createDescription')" :close-label="t('common.close')">
    <div class="grid gap-4" :class="credentialForm.type === 'private_key' ? 'h-full min-h-0' : ''">
      <CredentialFormFields
        v-model:input="credentialForm"
        :editing="false"
        :type-changed="false"
        :type-options="credentialTypeOptions"
        :errors="credentialValidation"
        :labels="credentialFormLabels"
      />
      <div v-if="credentialActionError" class="rounded-control border border-danger-border bg-danger-bg p-3 text-body text-danger">{{ credentialActionError }}</div>
    </div>
    <template #footer>
      <Button variant="secondary" @click="credentialDialogOpen = false">{{ t('common.cancel') }}</Button>
      <Button variant="primary" :loading="credentialSaving" :disabled="Boolean(Object.keys(credentialValidation).length)" @click="saveQuickCredential">{{ t('common.create') }}</Button>
    </template>
  </Dialog>
</template>
