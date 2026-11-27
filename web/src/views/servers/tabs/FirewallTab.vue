<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { MoreHorizontal, Plus, RefreshCcw, Trash2 } from '@/icons';
import { securityApi } from '@/api/security';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import ConfirmDialog from '@/components/ui/ConfirmDialog.vue';
import Dialog from '@/components/ui/Dialog.vue';
import Dropdown from '@/components/ui/Dropdown.vue';
import DropdownItem from '@/components/ui/DropdownItem.vue';
import EmptyState from '@/components/ui/EmptyState.vue';
import IconButton from '@/components/ui/IconButton.vue';
import Input from '@/components/ui/Input.vue';
import NumberBox from '@/components/ui/NumberBox.vue';
import Select from '@/components/ui/Select.vue';
import Table from '@/components/ui/Table.vue';
import { useErrorDialog } from '@/components/ui/errorDialog';
import { useSuccessToast } from '@/components/ui/toast';
import FormField from '@/components/patterns/FormField.vue';
import { useI18n } from '@/i18n';
import type { UfwRule, UfwState } from '@/types/security';
import type { ServerDto } from '@/types/servers';
import { createLatestRequestGuard, isAbortError } from '@/views/_shared/requestState';
import { ufwTone, validateUfwRule } from './securityModel';

const props = defineProps<{ server: ServerDto }>();
const { t } = useI18n();
const notifyError = useErrorDialog();
const notifySuccess = useSuccessToast();

const ufwState = ref<UfwState | null>(null);
const ufwLoading = ref(false);
const ufwError = ref('');
const ruleDialog = ref(false);
const pending = ref('');
const confirmTarget = ref<{ kind: 'delete-rule'; rule: UfwRule } | null>(null);
const ruleForm = reactive({ port: '443', protocol: 'tcp', from: 'Anywhere' });

const ufwGuard = createLatestRequestGuard();
let ufwController: AbortController | null = null;

const ufwRuleValidation = computed(() => validateUfwRule({ port: Number(ruleForm.port), protocol: ruleForm.protocol, from: ruleForm.from }));

/** UFW 端口是 1..65535 的整数（不含范围文本），NumberBox 直接承载；draft 保持 string。 */
const rulePortValue = computed<number | null>(() => {
  const trimmed = ruleForm.port.trim();
  if (!trimmed) return null;
  const parsed = Number(trimmed);
  return Number.isFinite(parsed) ? parsed : null;
});

function updateRulePort(value: number | null) {
  ruleForm.port = value === null ? '' : String(value);
}

const ufwColumns = computed(() => [
  { key: 'number', label: t('securityPage.ruleNumber'), nowrap: true, width: 'w-16', hideBelow: 'sm' as const },
  { key: 'action', label: t('securityPage.action'), nowrap: true, width: 'w-28' },
  { key: 'details', label: t('common.details') },
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
  } catch (err) {
    if (isAbortError(err)) return;
    ufwError.value = err instanceof Error ? err.message : t('securityPage.loadPanelFailed');
  } finally {
    if (ufwGuard.isCurrent(requestId)) ufwLoading.value = false;
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
    ufwState.value = await securityApi.deleteUfwRule(props.server.id, target.rule.number);
    notifySuccess(t('securityPage.ruleDeleted'), ufwState.value);
    confirmTarget.value = null;
  });
}

onMounted(() => {
  void loadUfw();
});
onBeforeUnmount(() => {
  ufwController?.abort();
});
</script>

<template>
  <div class="grid h-full min-h-0 grid-rows-page gap-4">
    <div class="grid h-full min-h-0 grid-rows-page-top gap-3">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <div class="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
          <Badge :tone="ufwTone(ufwState)">{{ ufwState?.status || t('state.unknown') }}</Badge>
          <span>{{ ufwState?.defaultPolicy || t('securityPage.noDefaultPolicy') }}</span>
          <span class="hidden wide:inline">{{ t('securityPage.managedAutomatically') }}</span>
        </div>
        <div class="flex flex-wrap gap-2">
          <Button v-permission="'servers.execute'" size="sm" variant="primary" :disabled="!ufwState?.supported" @click="openRuleDialog"><Plus />{{ t('securityPage.addRule') }}</Button>
          <Button size="sm" :loading="ufwLoading" @click="loadUfw"><RefreshCcw />{{ t('common.refresh') }}</Button>
        </div>
      </div>
      <div class="rounded-control border border-warning-border bg-warning-bg p-3 text-sm text-warning wide:hidden">{{ t('securityPage.managedAutomatically') }}</div>

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
        row-height="auto"
        :loading="ufwLoading"
        :loading-label="t('common.loading')"
      >
        <template #number="{ row }">
          <span class="whitespace-nowrap text-caption font-semibold text-muted-foreground">#{{ row.number }}</span>
        </template>
        <template #action="{ row }">
          <strong class="whitespace-nowrap text-foreground">{{ row.action }}</strong>
        </template>
        <template #details="{ row }">
          <div class="grid min-w-0 gap-0.5 text-caption text-muted-foreground">
            <span class="truncate whitespace-nowrap">{{ t('securityPage.to') }}: {{ row.to }}</span>
            <span class="truncate whitespace-nowrap">{{ t('securityPage.from') }}: {{ row.from }}</span>
          </div>
        </template>
        <template #actions="{ row }">
          <div class="flex items-center justify-end gap-1">
            <Button class="max-wide:hidden" size="sm" variant="danger" @click="confirmTarget = { kind: 'delete-rule', rule: row }"><Trash2 />{{ t('common.delete') }}</Button>
            <Dropdown class="wide:hidden" align="right">
              <template #trigger>
                <IconButton size="sm" :label="t('common.moreActions')"><MoreHorizontal /></IconButton>
              </template>
              <DropdownItem tone="danger" @click="confirmTarget = { kind: 'delete-rule', rule: row }"><Trash2 />{{ t('common.delete') }}</DropdownItem>
            </Dropdown>
          </div>
        </template>
        <template #empty>
          <EmptyState variant="inset" :title="t('securityPage.noRules')" :description="t('securityPage.noRulesHint')" />
        </template>
      </Table>
    </div>

    <Dialog v-model:open="ruleDialog" :title="t('securityPage.addRule')" :description="t('securityPage.addRuleDescription')" :close-label="t('common.close')">
      <div class="grid gap-3">
        <FormField
          id="ufw-rule-port"
          v-slot="{ id, describedBy, invalid }"
          :label="t('securityPage.port')"
          :error="ufwRuleValidation.port ? t(ufwRuleValidation.port) : ''"
        >
          <NumberBox
            :id="id"
            :model-value="rulePortValue"
            :aria-describedby="describedBy"
            :invalid="invalid"
            :min="1"
            :max="65535"
            :step="1"
            @update:model-value="updateRulePort"
          />
        </FormField>
        <label class="grid gap-1 text-sm">{{ t('securityPage.protocol') }}<Select v-model="ruleForm.protocol" :options="[{ label: 'TCP', value: 'tcp' }, { label: 'UDP', value: 'udp' }]" /></label>
        <label class="grid gap-1 text-sm">{{ t('securityPage.from') }}<Input v-model="ruleForm.from" :invalid="Boolean(ufwRuleValidation.from)" /></label>
        <div v-if="Object.values(ufwRuleValidation).length" class="rounded-control border border-warning-border bg-warning-bg p-3 text-sm text-warning">{{ t(Object.values(ufwRuleValidation)[0] || 'securityPage.validationGeneric') }}</div>
      </div>
      <template #footer>
        <Button @click="ruleDialog = false">{{ t('common.cancel') }}</Button>
        <Button variant="primary" :loading="pending === 'add-rule'" :disabled="Boolean(Object.keys(ufwRuleValidation).length)" @click="addRule">{{ t('common.create') }}</Button>
      </template>
    </Dialog>

    <ConfirmDialog
      :open="Boolean(confirmTarget)"
      :title="t('securityPage.deleteRule')"
      :description="t('securityPage.confirmDescription')"
      :impact="t('securityPage.confirmImpact')"
      tone="danger"
      :loading="Boolean(pending)"
      :confirm-label="t('common.apply')"
      :cancel-label="t('common.cancel')"
      checkbox-label=""
      @update:open="(value) => { if (!value) confirmTarget = null; }"
      @confirm="confirmAction"
      @cancel="confirmTarget = null"
    />
  </div>
</template>
