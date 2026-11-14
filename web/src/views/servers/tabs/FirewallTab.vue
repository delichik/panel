<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { AlertTriangle, FileCode2, LockKeyhole, MoreHorizontal, Plus, RefreshCcw, Save, ShieldCheck, Trash2, UnlockKeyhole } from '@lucide/vue';
import { securityApi } from '@/api/security';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import CodeEditor from '@/components/ui/CodeEditor.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import Dropdown from '@/components/ui/Dropdown.vue';
import DropdownItem from '@/components/ui/DropdownItem.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import IconButton from '@/components/ui/IconButton.vue';
import Input from '@/components/ui/Input.vue';
import Select from '@/components/ui/Select.vue';
import Table from '@/components/ui/Table.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { Fail2BanJail, Fail2BanState, UfwRule, UfwState } from '@/types/security';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';
import type { FirewallView } from '../model';
import { fail2BanPreset, fail2BanTone, hasAdvancedJailConfig, jailsToYaml, parseSimpleJailsFromYaml, ufwTone, validateUfwRule } from './securityModel';

const props = defineProps<{ server: ServerDto; view: FirewallView }>();
const emit = defineEmits<{ 'update:view': [value: FirewallView] }>();
const { t } = useI18n();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const isDev = import.meta.env.DEV;
const subTabs = computed(() => [
  { value: 'ufw', label: t('securityPage.ufw') },
  ...(isDev ? [{ value: 'fail2ban', label: t('routes.fail2ban.title') }] : []),
]);

const ufwState = ref<UfwState | null>(null);
const ufwLoading = ref(false);
const ufwError = ref('');
const ufwLoaded = ref(false);
const fail2banState = ref<Fail2BanState | null>(null);
const fail2banLoading = ref(false);
const fail2banError = ref('');
const fail2banLoaded = ref(false);
const yamlMode = ref(false);
const yamlDraft = ref('');
const jailDrafts = ref<Fail2BanJail[]>([]);
const ruleDialog = ref(false);
const yamlDiscardConfirm = ref(false);
const pending = ref('');
const confirmTarget = ref<{ kind: 'delete-rule'; rule: UfwRule } | { kind: 'enable-fail2ban' } | { kind: 'release-fail2ban' } | null>(null);
const ruleForm = reactive({ port: '443', protocol: 'tcp', from: 'Anywhere' });

const ufwGuard = createLatestRequestGuard();
const fail2banGuard = createLatestRequestGuard();
let ufwController: AbortController | null = null;
let fail2banController: AbortController | null = null;

const ufwRuleValidation = computed(() => validateUfwRule({ port: Number(ruleForm.port), protocol: ruleForm.protocol, from: ruleForm.from }));
const hasYamlChanges = computed(() => fail2banState.value ? yamlDraft.value.trim() !== (fail2banState.value.configYaml ?? '').trim() : false);
const runtimeJails = computed(() => fail2banState.value?.jails ?? []);
const takeoverRequired = computed(() => Boolean(fail2banState.value?.installed && !fail2banState.value?.managed));

const ufwColumns = computed(() => [
  { key: 'number', label: t('securityPage.ruleNumber'), nowrap: true, width: 'w-16' },
  { key: 'action', label: t('securityPage.action'), nowrap: true },
  { key: 'to', label: t('securityPage.to') },
  { key: 'from', label: t('securityPage.from') },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);

const jailColumns = computed(() => [
  { key: 'name', label: t('common.name') },
  { key: 'state', label: t('common.status'), nowrap: true },
  { key: 'port', label: t('securityPage.port'), nowrap: true },
  { key: 'maxretry', label: t('securityPage.maxRetry'), nowrap: true },
  { key: 'findtime', label: t('securityPage.findTime'), nowrap: true },
  { key: 'bantime', label: t('securityPage.banTime'), nowrap: true },
  { key: 'actions', label: t('common.actions'), align: 'right' as const, nowrap: true },
]);

async function loadUfw() {
  ufwController?.abort();
  const requestId = ufwGuard.begin();
  const controller = new AbortController();
  ufwController = controller;
  ufwLoading.value = true;
  ufwError.value = '';
  try {
    const result = await securityApi.ufwState(props.server.id, { signal: controller.signal });
    if (!ufwGuard.isCurrent(requestId)) return;
    ufwState.value = result;
    ufwLoaded.value = true;
  } catch (err) {
    if (isAbortError(err)) return;
    ufwError.value = err instanceof Error ? err.message : t('securityPage.loadPanelFailed');
    notifyError(err instanceof Error ? err.message : t('securityPage.loadPanelFailed'), err);
  } finally {
    if (ufwGuard.isCurrent(requestId)) ufwLoading.value = false;
  }
}

async function loadFail2Ban() {
  fail2banController?.abort();
  const requestId = fail2banGuard.begin();
  const controller = new AbortController();
  fail2banController = controller;
  fail2banLoading.value = true;
  fail2banError.value = '';
  try {
    const result = await securityApi.fail2BanState(props.server.id, { signal: controller.signal });
    if (!fail2banGuard.isCurrent(requestId)) return;
    fail2banState.value = result;
    yamlDraft.value = result.configYaml ?? '';
    const configJails = result.config.jails ?? [];
    jailDrafts.value = configJails.length ? [...configJails] : parseSimpleJailsFromYaml(result.configYaml ?? '');
    fail2banLoaded.value = true;
  } catch (err) {
    if (isAbortError(err)) return;
    fail2banError.value = err instanceof Error ? err.message : t('securityPage.loadPanelFailed');
    notifyError(err instanceof Error ? err.message : t('securityPage.loadPanelFailed'), err);
  } finally {
    if (fail2banGuard.isCurrent(requestId)) fail2banLoading.value = false;
  }
}

async function run(operation: string, action: () => Promise<void>) {
  pending.value = operation;
  try {
    await action();
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    pending.value = '';
  }
}

function openRuleDialog() {
  Object.assign(ruleForm, { port: '443', protocol: 'tcp', from: 'Anywhere' });
  ruleDialog.value = true;
}

async function addRule() {
  if (Object.keys(ufwRuleValidation.value).length) return;
  await run('add-rule', async () => {
    ufwState.value = await securityApi.addUfwRule(props.server.id, { port: Number(ruleForm.port), protocol: ruleForm.protocol, from: ruleForm.from });
    notifySuccess(t('securityPage.ruleAdded'), ufwState.value);
    ruleDialog.value = false;
  });
}

async function confirmAction() {
  const target = confirmTarget.value;
  if (!target) return;
  await run(target.kind, async () => {
    if (target.kind === 'delete-rule') {
      ufwState.value = await securityApi.deleteUfwRule(props.server.id, target.rule.number);
      notifySuccess(t('securityPage.ruleDeleted'), ufwState.value);
    } else if (target.kind === 'enable-fail2ban') {
      // ConfirmDialog 的确认按钮在要求勾选时会被 checkbox 门禁，收到 confirm 即表示已勾选接管。
      const accepted = await securityApi.enableFail2Ban(props.server.id, { configYaml: yamlDraft.value, confirmTakeover: takeoverRequired.value });
      notifySuccess(t('securityPage.taskAccepted', { taskId: accepted.taskId }), accepted);
      await loadFail2Ban();
    } else {
      const accepted = await securityApi.releaseFail2Ban(props.server.id);
      notifySuccess(t('securityPage.taskAccepted', { taskId: accepted.taskId }), accepted);
      await loadFail2Ban();
    }
    confirmTarget.value = null;
  });
}

async function saveFail2BanDraft() {
  await run('save-fail2ban', async () => {
    fail2banState.value = await securityApi.saveFail2Ban(props.server.id, yamlDraft.value);
    yamlDraft.value = fail2banState.value.configYaml ?? '';
    jailDrafts.value = fail2banState.value.config.jails ?? [];
    notifySuccess(t('securityPage.draftSaved'), fail2banState.value);
  });
}

async function installFail2Ban() {
  await run('install-fail2ban', async () => {
    const accepted = await securityApi.installFail2Ban(props.server.id);
    notifySuccess(t('securityPage.taskAccepted', { taskId: accepted.taskId }), accepted);
    await loadFail2Ban();
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

watch(() => props.view, (view) => {
  if (view === 'fail2ban' && isDev && !fail2banLoaded.value) void loadFail2Ban();
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

function confirmTitle() {
  if (confirmTarget.value?.kind === 'delete-rule') return t('securityPage.deleteRule');
  if (confirmTarget.value?.kind === 'release-fail2ban') return t('securityPage.releaseFail2Ban');
  return t('securityPage.enableFail2Ban');
}

onMounted(() => {
  void loadUfw();
  if (props.view === 'fail2ban' && isDev) void loadFail2Ban();
});
onBeforeUnmount(() => {
  ufwController?.abort();
  fail2banController?.abort();
});
</script>

<template>
  <div class="grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] gap-4">
    <div v-if="subTabs.length > 1" class="inline-flex w-fit max-w-full overflow-x-auto rounded-xl border border-border bg-muted p-1" role="tablist" :aria-label="t('serversPage.tab.firewall')">
      <button
        v-for="tab in subTabs"
        :key="tab.value"
        type="button"
        role="tab"
        class="motion-tab h-8 rounded-lg px-3 text-sm font-medium text-muted-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40 aria-selected:bg-brand-bg aria-selected:text-brand"
        :aria-selected="view === tab.value"
        :tabindex="view === tab.value ? 0 : -1"
        @click="emit('update:view', tab.value as FirewallView)"
      >
        {{ tab.label }}
      </button>
    </div>

    <div v-show="view === 'ufw'" class="grid h-full min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] gap-3">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <div class="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
          <Badge :tone="ufwTone(ufwState)">{{ ufwState?.status || t('state.unknown') }}</Badge>
          <span>{{ ufwState?.defaultPolicy || t('securityPage.noDefaultPolicy') }}</span>
          <span class="hidden xl:inline">{{ t('securityPage.managedAutomatically') }}</span>
        </div>
        <div class="flex flex-wrap gap-2">
          <Button size="sm" variant="primary" :disabled="!ufwState?.supported" @click="openRuleDialog"><Plus />{{ t('securityPage.addRule') }}</Button>
          <Button size="sm" :loading="ufwLoading" @click="loadUfw"><RefreshCcw />{{ t('common.refresh') }}</Button>
        </div>
      </div>
      <div class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning xl:hidden">{{ t('securityPage.managedAutomatically') }}</div>

      <EmptyState v-if="ufwError && !ufwState" class="self-start" :title="t('common.loadFailed')" :description="ufwError">
        <template #actions>
          <Button size="sm" :loading="ufwLoading" @click="loadUfw"><RefreshCcw />{{ t('common.retry') }}</Button>
        </template>
      </EmptyState>

      <Table
        v-else
        :columns="ufwColumns"
        :rows="ufwState?.rules ?? []"
        row-key="number"
        :loading="ufwLoading"
        :loading-label="t('common.loading')"
      >
        <template #number="{ row }">
          <span class="text-xs font-semibold text-muted-foreground">#{{ row.number }}</span>
        </template>
        <template #action="{ row }">
          <strong class="text-foreground">{{ row.action }}</strong>
        </template>
        <template #to="{ row }">
          <span class="text-muted-foreground">{{ row.to }}</span>
        </template>
        <template #from="{ row }">
          <span class="text-muted-foreground">{{ row.from }}</span>
        </template>
        <template #actions="{ row }">
          <Button size="sm" variant="danger" @click="confirmTarget = { kind: 'delete-rule', rule: row }"><Trash2 />{{ t('common.delete') }}</Button>
        </template>
        <template #empty>
          <EmptyState :title="t('securityPage.noRules')" :description="t('securityPage.noRulesHint')" />
        </template>
      </Table>
    </div>

    <div v-show="view === 'fail2ban'" class="grid h-full min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] gap-3">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <div class="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
          <Badge :tone="fail2BanTone(fail2banState)">{{ fail2banState?.managed ? t('securityPage.managed') : t('securityPage.notManaged') }}</Badge>
          <span>{{ t('securityPage.installed') }}: {{ fail2banState?.installed ? t('state.healthy') : t('state.warning') }}</span>
          <span>{{ t('securityPage.active') }}: {{ fail2banState?.active ? t('state.healthy') : t('state.warning') }}</span>
          <span>{{ t('securityPage.updatedAt') }}: {{ formatDateTime(fail2banState?.updatedAt) || t('common.never') }}</span>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <Button size="sm" variant="primary" :loading="pending === 'enable-fail2ban'" @click="confirmTarget = { kind: 'enable-fail2ban' }"><ShieldCheck />{{ t('securityPage.enableFail2Ban') }}</Button>
          <Button size="sm" :loading="fail2banLoading" @click="loadFail2Ban"><RefreshCcw />{{ t('common.refresh') }}</Button>
          <Dropdown align="right">
            <template #trigger>
              <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
            </template>
            <DropdownItem :disabled="!fail2banState?.installed" @click="installFail2Ban"><LockKeyhole />{{ t('securityPage.installFail2Ban') }}</DropdownItem>
            <DropdownItem tone="danger" :disabled="!fail2banState?.managed" @click="confirmTarget = { kind: 'release-fail2ban' }"><UnlockKeyhole />{{ t('securityPage.releaseFail2Ban') }}</DropdownItem>
          </Dropdown>
        </div>
      </div>

      <div class="flex flex-wrap items-center justify-between gap-2">
        <div class="flex flex-wrap items-center gap-2">
          <Button size="sm" @click="applyPreset('ssh')">{{ t('securityPage.presetSsh') }}</Button>
          <Button size="sm" @click="applyPreset('nginx-auth')">{{ t('securityPage.presetNginx') }}</Button>
          <Button size="sm" @click="applyPreset('recidive')">{{ t('securityPage.presetRecidive') }}</Button>
          <Button size="sm" @click="yamlMode = !yamlMode"><FileCode2 />{{ yamlMode ? t('securityPage.visualMode') : t('securityPage.yamlMode') }}</Button>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <span class="flex items-center gap-2 text-xs text-muted-foreground"><AlertTriangle class="size-4 shrink-0" />{{ t('securityPage.fail2banConfirmHint') }}</span>
          <Button size="sm" variant="primary" :disabled="!hasYamlChanges" :loading="pending === 'save-fail2ban'" @click="saveFail2BanDraft"><Save />{{ t('securityPage.saveDraft') }}</Button>
        </div>
      </div>

      <div class="min-h-0">
        <div v-if="yamlMode" class="h-full min-h-0">
          <CodeEditor v-model="yamlDraft" language="yaml" :editor-label="t('securityPage.yamlMode')" />
        </div>
        <EmptyState v-else-if="fail2banError && !fail2banState" :title="t('common.loadFailed')" :description="fail2banError">
          <template #actions>
            <Button size="sm" :loading="fail2banLoading" @click="loadFail2Ban"><RefreshCcw />{{ t('common.retry') }}</Button>
          </template>
        </EmptyState>
        <Table
          v-else
          :columns="jailColumns"
          :rows="jailDrafts"
          row-key="name"
          :loading="fail2banLoading"
          :loading-label="t('common.loading')"
        >
          <template #name="{ row }">
            <div class="min-w-0">
              <strong class="block truncate text-foreground">{{ row.name }}</strong>
              <span class="block truncate text-xs text-muted-foreground">{{ row.filter || row.preset || t('common.notAvailable') }} · {{ row.logpath || t('common.notAvailable') }}</span>
            </div>
          </template>
          <template #state="{ row }">
            <Badge :tone="row.enabled ? 'success' : 'neutral'">{{ row.enabled ? t('securityPage.enabled') : t('securityPage.disabled') }}</Badge>
          </template>
          <template #port="{ row }">
            <span class="text-muted-foreground">{{ row.port || '-' }}</span>
          </template>
          <template #maxretry="{ row }">
            <span class="text-muted-foreground">{{ row.maxretry ?? '-' }}</span>
          </template>
          <template #findtime="{ row }">
            <span class="text-muted-foreground">{{ row.findtime || '-' }}</span>
          </template>
          <template #bantime="{ row }">
            <span class="text-muted-foreground">{{ row.bantime || '-' }}</span>
          </template>
          <template #actions="{ row }">
            <Button size="sm" variant="danger" @click="removeJail(row.name)"><Trash2 />{{ t('common.delete') }}</Button>
          </template>
          <template #empty>
            <EmptyState :title="t('securityPage.noJails')" :description="t('securityPage.noJailsHint')" />
          </template>
        </Table>
      </div>
    </div>

    <Dialog v-model:open="ruleDialog" :title="t('securityPage.addRule')" :description="t('securityPage.addRuleDescription')" :close-label="t('common.close')">
      <div class="grid gap-3">
        <label class="grid gap-1 text-sm">{{ t('securityPage.port') }}<Input v-model="ruleForm.port" type="number" :invalid="Boolean(ufwRuleValidation.port)" /></label>
        <label class="grid gap-1 text-sm">{{ t('securityPage.protocol') }}<Select v-model="ruleForm.protocol" :options="[{ label: 'TCP', value: 'tcp' }, { label: 'UDP', value: 'udp' }]" /></label>
        <label class="grid gap-1 text-sm">{{ t('securityPage.from') }}<Input v-model="ruleForm.from" :invalid="Boolean(ufwRuleValidation.from)" /></label>
        <div v-if="Object.values(ufwRuleValidation).length" class="rounded-xl border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ t(Object.values(ufwRuleValidation)[0] || 'securityPage.validationGeneric') }}</div>
      </div>
      <template #footer>
        <Button @click="ruleDialog = false">{{ t('common.cancel') }}</Button>
        <Button variant="primary" :loading="pending === 'add-rule'" :disabled="Boolean(Object.keys(ufwRuleValidation).length)" @click="addRule">{{ t('common.create') }}</Button>
      </template>
    </Dialog>

    <ConfirmDialog
      :open="Boolean(confirmTarget)"
      :title="confirmTitle()"
      :description="t('securityPage.confirmDescription')"
      :impact="confirmTarget?.kind === 'delete-rule' ? t('securityPage.confirmImpact') : t('securityPage.fail2banConfirmHint')"
      :tone="confirmTarget?.kind === 'enable-fail2ban' ? 'warning' : 'danger'"
      :loading="Boolean(pending)"
      :confirm-label="t('common.apply')"
      :cancel-label="t('common.cancel')"
      :require-checkbox="confirmTarget?.kind === 'enable-fail2ban' && takeoverRequired"
      :checkbox-label="confirmTarget?.kind === 'enable-fail2ban' ? t('securityPage.confirmTakeover') : ''"
      @update:open="(value) => { if (!value) confirmTarget = null; }"
      @confirm="confirmAction"
      @cancel="confirmTarget = null"
    />

    <ConfirmDialog
      :open="yamlDiscardConfirm"
      :title="t('securityPage.discardAdvancedConfig')"
      :impact="t('securityPage.discardAdvancedConfigImpact')"
      tone="warning"
      :confirm-label="t('common.discard')"
      :cancel-label="t('common.cancel')"
      checkbox-label=""
      @confirm="confirmYamlToVisual"
      @update:open="(open) => { if (!open) cancelYamlToVisual(); }"
    />
  </div>
</template>
