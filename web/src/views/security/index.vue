<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { AlertTriangle, FileCode2, LockKeyhole, Plus, RefreshCcw, Save, Shield, ShieldCheck, ShieldOff, Trash2, UnlockKeyhole } from '@lucide/vue';
import { securityApi } from '@/api/security';
import { serversApi } from '@/api/servers';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import CodeEditor from '@/components/ui/CodeEditor.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import Input from '@/components/ui/Input.vue';
import Select from '@/components/ui/Select.vue';
import ServerContextSelector from '@/components/patterns/ServerContextSelector.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import ConsolePage from '@/components/templates/ConsolePage.vue';
import MasterDetailLayout from '@/components/templates/MasterDetailLayout.vue';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/servers';
import type { Fail2BanJail, Fail2BanState, UfwRule, UfwState } from '@/types/security';
import { formatDateTime } from '@/utils/datetime';
import { fail2BanPreset, fail2BanTone, hasAdvancedJailConfig, jailsToYaml, parseSimpleJailsFromYaml, serverOptionState, ufwTone, validateUfwRule } from './model';

const { t } = useI18n();
const route = useRoute();
const router = useRouter();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

let serversController: AbortController | null = null;
let panelController: AbortController | null = null;
let serversRequestId = 0;
let panelRequestId = 0;

const servers = ref<ServerDto[]>([]);
const selectedId = ref(String(route.query.server ?? ''));
const activeTab = computed<'ufw' | 'fail2ban'>(() => route.path.includes('/fail2ban') ? 'fail2ban' : 'ufw');
const loadingServers = ref(false);
const loadingPanel = ref(false);
const error = ref('');
const actionError = ref('');
const pending = ref('');

const ufwState = ref<UfwState | null>(null);
const fail2banState = ref<Fail2BanState | null>(null);
const yamlMode = ref(false);
const yamlDraft = ref('');
const jailDrafts = ref<Fail2BanJail[]>([]);
const ruleDialog = ref(false);
const confirmDialog = ref(false);
const confirmKind = ref<'enable-ufw' | 'install-ufw' | 'delete-rule' | 'enable-fail2ban' | 'release-fail2ban' | ''>('');
const targetRule = ref<UfwRule | null>(null);
const takeoverConfirmed = ref(false);
const yamlDiscardConfirm = ref(false);

const ruleForm = reactive({ port: '443', protocol: 'tcp', from: 'Anywhere' });

const selectedServer = computed(() => servers.value.find((item) => item.id === selectedId.value) ?? null);
const serverContextOptions = computed(() => servers.value.map((server) => {
  const state = serverOptionState(server);
  return {
    id: server.id,
    name: server.name,
    description: server.host,
    status: state.canManageSecurity ? 'online' : state.reachable ? 'warning' : 'offline',
    statusLabel: state.canManageSecurity ? t('securityPage.manageable') : t(state.reachable ? 'securityPage.serverLimited' : 'securityPage.serverUnreachable'),
    statusTone: state.canManageSecurity ? 'success' as const : state.reachable ? 'warning' as const : 'danger' as const,
    capabilities: [
      state.agentReady ? 'Agent' : '',
      state.privileged ? 'sudo' : '',
    ].filter(Boolean),
    disabledReason: state.reachable ? '' : t('securityPage.serverUnreachable'),
  };
}));
const selectedServerState = computed(() => selectedServer.value ? serverOptionState(selectedServer.value) : null);
const ruleValidation = computed(() => validateUfwRule({ port: Number(ruleForm.port), protocol: ruleForm.protocol, from: ruleForm.from }));
const hasYamlChanges = computed(() => fail2banState.value ? yamlDraft.value.trim() !== (fail2banState.value.configYaml ?? '').trim() : false);
const pageTitleKey = computed(() => activeTab.value === 'fail2ban' ? 'routes.fail2ban.title' : 'routes.firewall.title');
const pageDescriptionKey = computed(() => activeTab.value === 'fail2ban' ? 'routes.fail2ban.description' : 'routes.firewall.description');
const runtimeJails = computed(() => fail2banState.value?.jails ?? []);

watch(() => route.path, () => {
  void loadPanel({ clear: true });
});

watch(selectedId, (value) => {
  void router.replace({ query: { ...route.query, server: value || undefined } });
  void loadPanel({ clear: true });
});

watch(yamlMode, (enabled) => {
  if (enabled) return;
  if (hasAdvancedJailConfig(yamlDraft.value)) {
    yamlDiscardConfirm.value = true;
    return;
  }
  jailDrafts.value = parseSimpleJailsFromYaml(yamlDraft.value);
});

function confirmYamlToVisual() {
  yamlDiscardConfirm.value = false;
  jailDrafts.value = parseSimpleJailsFromYaml(yamlDraft.value);
}

function cancelYamlToVisual() {
  yamlDiscardConfirm.value = false;
  yamlMode.value = true;
}

async function loadServers() {
  serversController?.abort();
  const requestId = ++serversRequestId;
  const controller = new AbortController();
  serversController = controller;
  loadingServers.value = true;
  error.value = '';
  try {
    const nextServers = await serversApi.list({ signal: controller.signal });
    if (requestId !== serversRequestId) return;
    servers.value = nextServers;
    if (!servers.value.some((item) => item.id === selectedId.value)) {
      selectedId.value = servers.value[0]?.id ?? '';
      if (!selectedId.value) {
        clearPanelState();
        panelController?.abort();
        panelRequestId += 1;
        loadingPanel.value = false;
      }
    }
  } catch (err) {
    if (isAbortError(err)) return;
    error.value = err instanceof Error ? err.message : t('securityPage.loadServersFailed');
    notifyError(err instanceof Error ? err.message : t('securityPage.loadServersFailed'));
  } finally {
    if (requestId === serversRequestId) loadingServers.value = false;
  }
}

async function loadPanel(options: { clear?: boolean } = {}) {
  panelController?.abort();
  const requestId = ++panelRequestId;
  const server = selectedServer.value;
  if (!server) {
    loadingPanel.value = false;
    if (options.clear) clearPanelState();
    return;
  }
  const controller = new AbortController();
  panelController = controller;
  const tab = activeTab.value;
  if (options.clear) {
    if (tab === 'ufw') ufwState.value = null;
    else {
      fail2banState.value = null;
      yamlDraft.value = '';
      jailDrafts.value = [];
    }
  }
  loadingPanel.value = true;
  actionError.value = '';
  try {
    if (tab === 'ufw') {
      const result = await securityApi.ufwState(server.id, { signal: controller.signal });
      if (requestId !== panelRequestId || tab !== activeTab.value) return;
      ufwState.value = result;
    } else {
      const result = await securityApi.fail2BanState(server.id, { signal: controller.signal });
      if (requestId !== panelRequestId || tab !== activeTab.value) return;
      fail2banState.value = result;
      yamlDraft.value = fail2banState.value.configYaml ?? '';
      const configJails = fail2banState.value.config.jails ?? [];
      jailDrafts.value = configJails.length ? [...configJails] : parseSimpleJailsFromYaml(fail2banState.value.configYaml ?? '');
    }
  } catch (err) {
    if (isAbortError(err)) return;
    actionError.value = err instanceof Error ? err.message : t('securityPage.loadPanelFailed');
    notifyError(err instanceof Error ? err.message : t('securityPage.loadPanelFailed'));
    if (tab === 'ufw') ufwState.value = null;
    else fail2banState.value = null;
  } finally {
    if (requestId === panelRequestId) loadingPanel.value = false;
  }
}

async function refresh() {
  const previous = selectedId.value;
  await loadServers();
  if (selectedId.value === previous) await loadPanel();
}

function clearPanelState() {
  ufwState.value = null;
  fail2banState.value = null;
  yamlDraft.value = '';
  jailDrafts.value = [];
}

function openRuleDialog() {
  Object.assign(ruleForm, { port: '443', protocol: 'tcp', from: 'Anywhere' });
  ruleDialog.value = true;
}

async function addRule() {
  if (!selectedServer.value || Object.keys(ruleValidation.value).length) return;
  await run('add-rule', async () => {
    ufwState.value = await securityApi.addUfwRule(selectedServer.value!.id, { port: Number(ruleForm.port), protocol: ruleForm.protocol, from: ruleForm.from });
    notifySuccess(t('securityPage.ruleAdded'));
    ruleDialog.value = false;
  });
}

function ask(kind: typeof confirmKind.value, rule?: UfwRule) {
  confirmKind.value = kind;
  targetRule.value = rule ?? null;
  takeoverConfirmed.value = kind !== 'enable-fail2ban';
  confirmDialog.value = true;
}

async function confirmAction() {
  const server = selectedServer.value;
  if (!server) return;
  const kind = confirmKind.value;
  await run(kind || 'confirm', async () => {
    if (kind === 'enable-ufw') {
      const accepted = await securityApi.enableUfw(server.id);
      notifySuccess(t('securityPage.taskAccepted', { taskId: accepted.taskId }));
    }
    if (kind === 'install-ufw') {
      const accepted = await securityApi.installUfw(server.id);
      notifySuccess(t('securityPage.taskAccepted', { taskId: accepted.taskId }));
    }
    if (kind === 'delete-rule' && targetRule.value) {
      ufwState.value = await securityApi.deleteUfwRule(server.id, targetRule.value.number);
      notifySuccess(t('securityPage.ruleDeleted'));
    }
    if (kind === 'enable-fail2ban') {
      const accepted = await securityApi.enableFail2Ban(server.id, { configYaml: yamlDraft.value, confirmTakeover: takeoverConfirmed.value });
      notifySuccess(t('securityPage.taskAccepted', { taskId: accepted.taskId }));
    }
    if (kind === 'release-fail2ban') {
      const accepted = await securityApi.releaseFail2Ban(server.id);
      notifySuccess(t('securityPage.taskAccepted', { taskId: accepted.taskId }));
    }
    confirmDialog.value = false;
    await loadPanel();
  });
}

async function saveFail2BanDraft() {
  if (!selectedServer.value) return;
  await run('save-fail2ban', async () => {
    fail2banState.value = await securityApi.saveFail2Ban(selectedServer.value!.id, yamlDraft.value);
    yamlDraft.value = fail2banState.value.configYaml ?? '';
    jailDrafts.value = fail2banState.value.config.jails ?? [];
    notifySuccess(t('securityPage.draftSaved'));
  });
}

async function installFail2Ban() {
  if (!selectedServer.value) return;
  await run('install-fail2ban', async () => {
    const accepted = await securityApi.installFail2Ban(selectedServer.value!.id);
    notifySuccess(t('securityPage.taskAccepted', { taskId: accepted.taskId }));
    await loadPanel();
  });
}

function applyPreset(name: 'ssh' | 'nginx-auth' | 'recidive') {
  const preset = fail2BanPreset(name);
  const next = [...jailDrafts.value.filter((item) => item.name !== preset.name), preset];
  jailDrafts.value = next;
  yamlDraft.value = jailsToYaml(next);
}

function removeJail(name: string) {
  jailDrafts.value = jailDrafts.value.filter((item) => item.name !== name);
  yamlDraft.value = jailsToYaml(jailDrafts.value);
}

async function run(operation: string, action: () => Promise<void>) {
  pending.value = operation;
  actionError.value = '';
  try {
    await action();
  } catch (err) {
    actionError.value = err instanceof Error ? err.message : t('common.operationFailed');
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'));
  } finally {
    pending.value = '';
  }
}

function confirmTitle() {
  if (confirmKind.value === 'delete-rule') return t('securityPage.deleteRule');
  if (confirmKind.value === 'release-fail2ban') return t('securityPage.releaseFail2Ban');
  if (confirmKind.value === 'enable-fail2ban') return t('securityPage.enableFail2Ban');
  return t('securityPage.enableFirewall');
}

function isAbortError(error: unknown) {
  return Boolean(error && typeof error === 'object' && 'code' in error && (error as { code?: string }).code === 'request_aborted');
}

onMounted(async () => {
  await loadServers();
  await loadPanel({ clear: true });
});

onBeforeUnmount(() => {
  serversController?.abort();
  panelController?.abort();
});
</script>

<template>
  <ConsolePage :title="t(pageTitleKey)" :description="t(pageDescriptionKey)">
    <template #actions>
      <Button size="sm" :loading="loadingServers || loadingPanel" @click="refresh"><RefreshCcw />{{ t('common.refresh') }}</Button>
    </template>

    <MasterDetailLayout class="h-full min-h-[640px]">
      <template #master>
      <aside class="grid min-h-0 min-w-0 grid-rows-[auto_auto_minmax(0,1fr)] rounded-2xl border border-border bg-card">
        <div class="border-b border-border p-4">
          <h2 class="m-0 text-sm font-semibold text-foreground">{{ t('securityPage.serverContext') }}</h2>
          <p class="m-0 mt-1 text-xs text-muted-foreground">{{ t('securityPage.selectServerHint') }}</p>
        </div>
        <div class="grid grid-cols-3 gap-2 border-b border-border p-4 text-center text-xs">
          <div class="rounded-xl border border-border bg-background p-3">
            <strong class="block text-lg text-foreground">{{ servers.length }}</strong>
            <span class="text-muted-foreground">{{ t('securityPage.nodes') }}</span>
          </div>
          <div class="rounded-xl border border-border bg-background p-3">
            <strong class="block text-lg text-foreground">{{ servers.filter((item) => item.traits?.['sys.ufw_active'] === 'true').length }}</strong>
            <span class="text-muted-foreground">UFW</span>
          </div>
          <div class="rounded-xl border border-border bg-background p-3">
            <strong class="block text-lg text-foreground">{{ servers.filter((item) => item.traits?.['agent.status'] === 'compatible').length }}</strong>
            <span class="text-muted-foreground">Agent</span>
          </div>
        </div>
        <div class="min-h-0 overflow-auto p-3">
          <ServerContextSelector
            v-model="selectedId"
            :servers="serverContextOptions"
            :label="t('securityPage.serverContext')"
            :loading="loadingServers"
            :disabled="loadingServers"
          />
        </div>
      </aside>
      </template>

      <template #detail>
      <main class="grid min-h-0 min-w-0">
        <EmptyState v-if="!selectedServer" :title="t('securityPage.selectServer')" :description="t('securityPage.selectServerHint')" />
        <article v-else class="grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden rounded-2xl border border-border bg-card">
          <header class="flex items-start justify-between gap-4 border-b border-border p-5 max-lg:grid">
            <div>
              <div class="flex flex-wrap items-center gap-2">
                <h2 class="m-0 text-xl font-semibold text-foreground">{{ selectedServer.name }}</h2>
                <Badge :tone="selectedServerState?.canManageSecurity ? 'success' : 'warning'">{{ selectedServer.host }}</Badge>
              </div>
              <p class="m-0 mt-1 text-sm text-muted-foreground">{{ t('securityPage.serverScopeDescription') }}</p>
            </div>
            <div class="flex flex-wrap gap-2">
              <Button v-if="activeTab === 'ufw'" size="sm" variant="primary" :disabled="!ufwState?.supported" @click="openRuleDialog"><Plus />{{ t('securityPage.addRule') }}</Button>
              <Button v-else size="sm" variant="primary" :disabled="!hasYamlChanges" :loading="pending === 'save-fail2ban'" @click="saveFail2BanDraft"><Save />{{ t('securityPage.saveDraft') }}</Button>
            </div>
          </header>

          <div class="min-h-0 p-5">
            <div v-if="activeTab === 'ufw'" class="grid h-full min-h-0 grid-rows-[minmax(0,1fr)] gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
              <section class="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] rounded-2xl border border-border bg-background">
                <div class="flex items-center justify-between gap-3 border-b border-border p-4">
                  <div class="flex items-center gap-3">
                    <ShieldCheck class="size-5 text-muted-foreground" />
                    <div>
                      <h3 class="m-0 text-sm font-semibold">{{ t('securityPage.ufwRules') }}</h3>
                      <p class="m-0 text-xs text-muted-foreground">{{ ufwState?.defaultPolicy || t('securityPage.noDefaultPolicy') }}</p>
                    </div>
                  </div>
                  <Badge :tone="ufwTone(ufwState)">{{ ufwState?.status || t('state.unknown') }}</Badge>
                </div>
                <div class="motion-stagger min-h-0 overflow-auto p-3">
                  <div v-if="loadingPanel && !ufwState" class="grid gap-2" aria-hidden="true">
                    <div v-for="item in 6" :key="item" class="grid grid-cols-[56px_minmax(0,1fr)_80px] items-center gap-3 rounded-xl border border-border p-3">
                      <div class="motion-skeleton h-4 w-8 rounded bg-muted animate-pulse" />
                      <div>
                        <div class="motion-skeleton h-4 w-40 rounded bg-muted animate-pulse" />
                        <div class="motion-skeleton mt-2 h-3 w-56 max-w-full rounded bg-muted animate-pulse" />
                      </div>
                      <div class="motion-skeleton h-8 w-20 rounded bg-muted animate-pulse" />
                    </div>
                  </div>
                  <EmptyState v-else-if="actionError && !ufwState?.rules.length" :title="t('common.loadFailed')" :description="actionError">
                    <template #actions>
                      <Button size="sm" :loading="loadingPanel" @click="refresh"><RefreshCcw />{{ t('common.retry') }}</Button>
                    </template>
                  </EmptyState>
                  <EmptyState v-else-if="!ufwState?.rules.length" :title="t('securityPage.noRules')" :description="t('securityPage.noRulesHint')" />
                  <div v-for="rule in ufwState?.rules" v-else :key="rule.number" class="motion-reveal mb-2 grid grid-cols-[56px_minmax(0,1fr)_auto] items-center gap-3 rounded-xl border border-border p-3">
                    <span class="text-xs font-semibold text-muted-foreground">#{{ rule.number }}</span>
                    <div class="min-w-0">
                      <strong class="block truncate text-sm text-foreground">{{ rule.action }} · {{ rule.to }}</strong>
                      <span class="block truncate text-xs text-muted-foreground">{{ t('securityPage.from') }} {{ rule.from }}</span>
                    </div>
                    <Button size="sm" variant="danger" @click="ask('delete-rule', rule)"><Trash2 />{{ t('common.delete') }}</Button>
                  </div>
                </div>
              </section>

              <aside class="grid content-start gap-3">
                <section class="rounded-2xl border border-border bg-background p-4">
                  <h3 class="m-0 flex items-center gap-2 text-sm font-semibold"><Shield class="size-4" />{{ t('securityPage.ufwPosture') }}</h3>
                  <dl class="mt-3 grid gap-2 text-sm">
                    <div><dt>{{ t('securityPage.supported') }}</dt><dd>{{ ufwState?.supported ? t('state.healthy') : t('state.critical') }}</dd></div>
                    <div><dt>{{ t('securityPage.installed') }}</dt><dd>{{ ufwState?.installed ? t('state.healthy') : t('state.warning') }}</dd></div>
                    <div><dt>{{ t('securityPage.active') }}</dt><dd>{{ ufwState?.active ? t('state.healthy') : t('state.warning') }}</dd></div>
                  </dl>
                </section>
                <section class="rounded-2xl border border-warning-border bg-warning-bg p-4 text-sm text-warning">
                  <div class="flex gap-2">
                    <AlertTriangle class="mt-0.5 size-4 shrink-0" />
                    <p class="m-0">{{ t('securityPage.ufwConfirmHint') }}</p>
                  </div>
                  <div class="mt-3 grid gap-2">
                    <Button :disabled="!ufwState?.supported || ufwState?.active" :loading="pending === 'enable-ufw'" @click="ask('enable-ufw')"><ShieldCheck />{{ t('securityPage.enableFirewall') }}</Button>
                    <Button :disabled="!ufwState?.supported || ufwState?.installed" :loading="pending === 'install-ufw'" @click="ask('install-ufw')"><Shield />{{ t('securityPage.installFirewall') }}</Button>
                  </div>
                </section>
              </aside>
            </div>

            <div v-else class="grid h-full min-h-0 grid-rows-[minmax(0,1fr)] gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
              <section class="grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] rounded-2xl border border-border bg-background">
                <div class="flex items-center justify-between gap-3 border-b border-border p-4">
                  <div>
                    <h3 class="m-0 text-sm font-semibold">{{ t('securityPage.fail2banRules') }}</h3>
                    <p class="m-0 text-xs text-muted-foreground">{{ t('securityPage.fail2banDraftHint') }}</p>
                  </div>
                  <Badge :tone="fail2BanTone(fail2banState)">{{ fail2banState?.managed ? t('securityPage.managed') : t('securityPage.notManaged') }}</Badge>
                </div>
                <div class="flex flex-wrap items-center justify-between gap-2 border-b border-border p-3">
                  <div class="flex flex-wrap gap-2">
                    <Button size="sm" @click="applyPreset('ssh')">{{ t('securityPage.presetSsh') }}</Button>
                    <Button size="sm" @click="applyPreset('nginx-auth')">{{ t('securityPage.presetNginx') }}</Button>
                    <Button size="sm" @click="applyPreset('recidive')">{{ t('securityPage.presetRecidive') }}</Button>
                  </div>
                  <Button size="sm" @click="yamlMode = !yamlMode"><FileCode2 />{{ yamlMode ? t('securityPage.visualMode') : t('securityPage.yamlMode') }}</Button>
                </div>
                <div class="min-h-0 p-4" :class="yamlMode ? 'overflow-hidden' : 'overflow-auto'">
                  <CodeEditor v-if="yamlMode" v-model="yamlDraft" language="yaml" :editor-label="t('securityPage.yamlMode')" />
                  <div v-else class="grid gap-3">
                    <div v-if="loadingPanel && !fail2banState" class="grid gap-3" aria-hidden="true">
                      <div v-for="item in 4" :key="item" class="grid gap-3 rounded-xl border border-border p-4">
                        <div class="flex items-start justify-between gap-3">
                          <div class="min-w-0 flex-1">
                            <div class="motion-skeleton h-4 w-32 rounded bg-muted animate-pulse" />
                            <div class="motion-skeleton mt-2 h-3 w-64 max-w-full rounded bg-muted animate-pulse" />
                          </div>
                          <div class="motion-skeleton h-6 w-20 rounded-full bg-muted animate-pulse" />
                        </div>
                        <div class="grid grid-cols-4 gap-3 max-lg:grid-cols-2">
                          <div v-for="line in 4" :key="line" class="motion-skeleton h-8 rounded bg-muted animate-pulse" />
                        </div>
                      </div>
                    </div>
                    <EmptyState v-else-if="actionError && !jailDrafts.length" :title="t('common.loadFailed')" :description="actionError">
                      <template #actions>
                        <Button size="sm" :loading="loadingPanel" @click="refresh"><RefreshCcw />{{ t('common.retry') }}</Button>
                      </template>
                    </EmptyState>
                    <EmptyState v-else-if="!jailDrafts.length" :title="t('securityPage.noJails')" :description="t('securityPage.noJailsHint')" />
                    <article v-for="jail in jailDrafts" v-else :key="jail.name" class="grid gap-3 rounded-xl border border-border p-4">
                      <div class="flex items-start justify-between gap-3">
                        <div>
                          <h4 class="m-0 text-sm font-semibold">{{ jail.name }}</h4>
                          <p class="m-0 mt-1 text-xs text-muted-foreground">{{ jail.filter || jail.preset || t('common.notAvailable') }} · {{ jail.logpath || t('common.notAvailable') }}</p>
                        </div>
                        <div class="flex gap-2">
                          <Badge :tone="jail.enabled ? 'success' : 'neutral'">{{ jail.enabled ? t('securityPage.enabled') : t('securityPage.disabled') }}</Badge>
                          <Button size="sm" variant="danger" @click="removeJail(jail.name)"><Trash2 />{{ t('common.delete') }}</Button>
                        </div>
                      </div>
                      <dl class="grid grid-cols-4 gap-3 text-xs max-lg:grid-cols-2">
                        <div><dt>{{ t('securityPage.port') }}</dt><dd>{{ jail.port || '-' }}</dd></div>
                        <div><dt>{{ t('securityPage.maxRetry') }}</dt><dd>{{ jail.maxretry ?? '-' }}</dd></div>
                        <div><dt>{{ t('securityPage.findTime') }}</dt><dd>{{ jail.findtime || '-' }}</dd></div>
                        <div><dt>{{ t('securityPage.banTime') }}</dt><dd>{{ jail.bantime || '-' }}</dd></div>
                      </dl>
                    </article>
                  </div>
                </div>
              </section>

              <aside class="grid content-start gap-3">
                <section class="rounded-2xl border border-border bg-background p-4">
                  <h3 class="m-0 flex items-center gap-2 text-sm font-semibold"><LockKeyhole class="size-4" />{{ t('securityPage.fail2banStatus') }}</h3>
                  <dl class="mt-3 grid gap-2 text-sm">
                    <div><dt>{{ t('securityPage.installed') }}</dt><dd>{{ fail2banState?.installed ? t('state.healthy') : t('state.warning') }}</dd></div>
                    <div><dt>{{ t('securityPage.active') }}</dt><dd>{{ fail2banState?.active ? t('state.healthy') : t('state.warning') }}</dd></div>
                    <div><dt>{{ t('securityPage.panelConfig') }}</dt><dd>{{ fail2banState?.panelConfigPresent ? t('state.healthy') : t('state.unknown') }}</dd></div>
                    <div><dt>{{ t('securityPage.updatedAt') }}</dt><dd>{{ formatDateTime(fail2banState?.updatedAt) || t('common.never') }}</dd></div>
                  </dl>
                </section>
                <section class="rounded-2xl border border-border bg-background p-4">
                  <h3 class="m-0 text-sm font-semibold">{{ t('securityPage.detectedJails') }}</h3>
                  <div class="mt-3 flex flex-wrap gap-2">
                    <Badge v-for="jail in runtimeJails" :key="jail" tone="info">{{ jail }}</Badge>
                    <span v-if="!runtimeJails.length" class="text-sm text-muted-foreground">{{ t('securityPage.noRuntimeJails') }}</span>
                  </div>
                </section>
                <section class="rounded-2xl border border-warning-border bg-warning-bg p-4 text-sm text-warning">
                  <div class="flex gap-2">
                    <AlertTriangle class="mt-0.5 size-4 shrink-0" />
                    <p class="m-0">{{ t('securityPage.fail2banConfirmHint') }}</p>
                  </div>
                  <div class="mt-3 grid gap-2">
                    <Button :loading="pending === 'install-fail2ban'" :disabled="fail2banState?.installed" @click="installFail2Ban"><LockKeyhole />{{ t('securityPage.installFail2Ban') }}</Button>
                    <Button :loading="pending === 'enable-fail2ban'" @click="ask('enable-fail2ban')"><ShieldCheck />{{ t('securityPage.enableFail2Ban') }}</Button>
                    <Button variant="danger" :loading="pending === 'release-fail2ban'" :disabled="!fail2banState?.managed" @click="ask('release-fail2ban')"><UnlockKeyhole />{{ t('securityPage.releaseFail2Ban') }}</Button>
                  </div>
                </section>
              </aside>
            </div>
          </div>
        </article>
      </main>
      </template>
    </MasterDetailLayout>

    <Dialog v-model:open="ruleDialog" :title="t('securityPage.addRule')" :description="t('securityPage.addRuleDescription')" :close-label="t('common.close')">
      <div class="grid gap-3">
        <label class="grid gap-1 text-sm">{{ t('securityPage.port') }}<Input v-model="ruleForm.port" type="number" :invalid="Boolean(ruleValidation.port)" /></label>
        <label class="grid gap-1 text-sm">{{ t('securityPage.protocol') }}<Select v-model="ruleForm.protocol" :options="[{ label: 'TCP', value: 'tcp' }, { label: 'UDP', value: 'udp' }]" /></label>
        <label class="grid gap-1 text-sm">{{ t('securityPage.from') }}<Input v-model="ruleForm.from" :invalid="Boolean(ruleValidation.from)" /></label>
        <div v-if="Object.values(ruleValidation).length" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ t(Object.values(ruleValidation)[0] || 'securityPage.validationGeneric') }}</div>
      </div>
      <template #footer>
        <Button @click="ruleDialog = false">{{ t('common.cancel') }}</Button>
        <Button variant="primary" :loading="pending === 'add-rule'" :disabled="Boolean(Object.keys(ruleValidation).length)" @click="addRule">{{ t('common.create') }}</Button>
      </template>
    </Dialog>

    <ConfirmDialog
      :open="yamlDiscardConfirm"
      :title="t('securityPage.discardAdvancedConfig')"
      :impact="t('securityPage.discardAdvancedConfigImpact')"
      tone="warning"
      :confirm-label="t('common.discard')"
      :cancel-label="t('common.cancel')"
      checkbox-label=""
      @confirm="confirmYamlToVisual"
      @update:open="(open) => { if (!open) cancelYamlToVisual() }"
    />

    <Dialog v-model:open="confirmDialog" :title="confirmTitle()" :description="t('securityPage.confirmDescription')" :close-label="t('common.close')">
      <div class="grid gap-3">
        <div class="flex gap-3 rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">
          <AlertTriangle class="mt-0.5 size-4 shrink-0" />
          <span>{{ t('securityPage.confirmImpact') }}</span>
        </div>
        <label v-if="confirmKind === 'enable-fail2ban' && fail2banState?.installed && !fail2banState?.managed" class="flex items-start gap-2 rounded-xl border border-border p-3 text-sm">
          <input v-model="takeoverConfirmed" class="mt-1" type="checkbox" />
          <span>{{ t('securityPage.confirmTakeover') }}</span>
        </label>
      </div>
      <template #footer>
        <Button @click="confirmDialog = false">{{ t('common.cancel') }}</Button>
        <Button :variant="confirmKind === 'delete-rule' || confirmKind === 'release-fail2ban' ? 'danger' : 'primary'" :disabled="confirmKind === 'enable-fail2ban' && fail2banState?.installed && !fail2banState?.managed && !takeoverConfirmed" :loading="Boolean(pending)" @click="confirmAction">
          <ShieldOff v-if="confirmKind === 'release-fail2ban'" />
          <Trash2 v-else-if="confirmKind === 'delete-rule'" />
          <ShieldCheck v-else />
          {{ t('common.apply') }}
        </Button>
      </template>
    </Dialog>
  </ConsolePage>
</template>

<style scoped>
dt {
  margin: 0 0 4px;
  color: var(--panel-text-muted);
  font-size: 12px;
}

dd {
  margin: 0;
  overflow-wrap: anywhere;
  color: var(--panel-text);
  font-weight: 600;
}
</style>
