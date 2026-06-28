<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import YAML from 'yaml';
import PageLoadingState from '@/components/PageLoadingState.vue';
import ServerSelector from '@/components/ServerSelector.vue';
import { serversApi } from '@/api/servers';
import { useI18n } from '@/i18n';
import type { Fail2BanConfigDto, Fail2BanJailDto, Fail2BanStateDto, ServerDto } from '@/types/api';

const { t } = useI18n();

type Fail2BanPreset = {
  value: string;
  titleKey: string;
  jail: Fail2BanJailDto;
};

const presetJails: Fail2BanPreset[] = [
  { value: 'ssh', titleKey: 'firewallPage.fail2banPresetSsh', jail: { name: 'sshd', enabled: true, preset: 'ssh', filter: 'sshd', port: 'ssh', logpath: '/var/log/auth.log', backend: 'systemd', maxretry: 5, findtime: '10m', bantime: '1h', ignoreip: ['127.0.0.1/8'], options: {} } },
  { value: 'nginx-http-auth', titleKey: 'firewallPage.fail2banPresetNginxAuth', jail: { name: 'nginx-http-auth', enabled: true, preset: 'nginx-http-auth', filter: 'nginx-http-auth', port: 'http,https', logpath: '/var/log/nginx/error.log', backend: 'auto', maxretry: 5, findtime: '10m', bantime: '1h', ignoreip: ['127.0.0.1/8'], options: {} } },
  { value: 'nginx-botsearch', titleKey: 'firewallPage.fail2banPresetNginxBot', jail: { name: 'nginx-botsearch', enabled: true, preset: 'nginx-botsearch', filter: 'nginx-botsearch', port: 'http,https', logpath: '/var/log/nginx/access.log', backend: 'auto', maxretry: 5, findtime: '10m', bantime: '1h', ignoreip: ['127.0.0.1/8'], options: {} } },
  { value: 'apache-auth', titleKey: 'firewallPage.fail2banPresetApacheAuth', jail: { name: 'apache-auth', enabled: true, preset: 'apache-auth', filter: 'apache-auth', port: 'http,https', logpath: '/var/log/apache2/error.log', backend: 'auto', maxretry: 5, findtime: '10m', bantime: '1h', ignoreip: ['127.0.0.1/8'], options: {} } },
  { value: 'postfix', titleKey: 'firewallPage.fail2banPresetPostfix', jail: { name: 'postfix', enabled: true, preset: 'postfix', filter: 'postfix', port: 'smtp,ssmtp,submission', logpath: '/var/log/mail.log', backend: 'auto', maxretry: 5, findtime: '10m', bantime: '1h', ignoreip: ['127.0.0.1/8'], options: {} } },
  { value: 'dovecot', titleKey: 'firewallPage.fail2banPresetDovecot', jail: { name: 'dovecot', enabled: true, preset: 'dovecot', filter: 'dovecot', port: 'pop3,pop3s,imap,imaps,submission,465,sieve', logpath: '/var/log/mail.log', backend: 'auto', maxretry: 5, findtime: '10m', bantime: '1h', ignoreip: ['127.0.0.1/8'], options: {} } },
  { value: 'custom', titleKey: 'firewallPage.fail2banPresetCustom', jail: { name: 'custom', enabled: true, preset: 'custom', filter: '', port: '', logpath: '', backend: 'auto', maxretry: 5, findtime: '10m', bantime: '1h', ignoreip: ['127.0.0.1/8'], options: {} } },
];

const servers = ref<ServerDto[]>([]);
const serverId = ref('');
const fail2banState = ref<Fail2BanStateDto | null>(null);
const loadingServers = ref(false);
const loadingState = ref(false);
const savingFail2Ban = ref(false);
const applyingFail2Ban = ref(false);
const releasingFail2Ban = ref(false);
const takeoverDialog = ref(false);
const releaseDialog = ref(false);
const error = ref('');

const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');
const snackbarTaskId = ref('');
const fail2banTab = ref<'rules' | 'raw'>('rules');
const fail2banYaml = ref('');
const fail2banForm = ref<Fail2BanConfigDto>({ jails: [] });
const fail2banRawError = ref('');
let stateRequestId = 0;

const selectedServer = computed(() => servers.value.find((server) => server.id === serverId.value) ?? null);
const hasPrivilege = computed(() => selectedServer.value?.privilege?.privileged === true || selectedServer.value?.sudo?.passwordless === true);
const canManageFail2Ban = computed(() => Boolean(selectedServer.value?.reachable && hasPrivilege.value));
const presetOptions = computed(() => presetJails.map((preset) => ({ value: preset.value, title: t(preset.titleKey) })));
const isManaged = computed(() => fail2banState.value?.managed === true);

function showMessage(text: string, color = 'success', taskId = '') {
  snackbarText.value = text;
  snackbarColor.value = color;
  snackbarTaskId.value = taskId;
  snackbar.value = true;
}

function taskRoute(taskId = snackbarTaskId.value) {
  return taskId ? { path: '/tasks', query: { task: taskId } } : '/tasks';
}

function fail2banStatusColor() {
  if (!fail2banState.value?.installed) return 'warning';
  if (!fail2banState.value.managed) return 'secondary';
  if (fail2banState.value.active) return 'success';
  return 'primary';
}

function fail2banStatusLabel() {
  if (!fail2banState.value) return t('common.unknown');
  if (!fail2banState.value.installed) return t('firewallPage.fail2banNotInstalled');
  if (!fail2banState.value.managed) return t('firewallPage.fail2banUnmanaged');
  if (fail2banState.value.active) return t('firewallPage.fail2banActive');
  return t('firewallPage.fail2banManagedInactive');
}

function primaryActionLabel() {
  if (!fail2banState.value?.installed) return t('firewallPage.fail2banInstall');
  if (!fail2banState.value.managed) return t('firewallPage.fail2banTakeover');
  return t('firewallPage.fail2banApply');
}

async function loadServers() {
  loadingServers.value = true;
  try {
    const rows = await serversApi.listServers();
    servers.value = rows;
    if (!serverId.value && rows.length) serverId.value = rows[0].id;
    if (serverId.value && !rows.some((server) => server.id === serverId.value)) serverId.value = rows[0]?.id ?? '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('firewallPage.loadFailed');
  } finally {
    loadingServers.value = false;
  }
}

async function loadState() {
  const requestedServerId = serverId.value;
  const requestId = ++stateRequestId;
  fail2banState.value = null;
  fail2banYaml.value = '';
  fail2banForm.value = { jails: [] };
  fail2banRawError.value = '';
  if (!requestedServerId) {
    loadingState.value = false;
    return;
  }
  loadingState.value = true;
  error.value = '';
  try {
    const result = await serversApi.fail2BanState(requestedServerId);
    if (requestId !== stateRequestId || serverId.value !== requestedServerId) return;
    applyFail2BanState(result);
  } catch (err) {
    if (requestId !== stateRequestId || serverId.value !== requestedServerId) return;
    error.value = err instanceof Error ? err.message : t('firewallPage.loadFailed');
  } finally {
    if (requestId === stateRequestId && serverId.value === requestedServerId) loadingState.value = false;
  }
}

function applyFail2BanState(result: Fail2BanStateDto) {
  fail2banState.value = result;
  fail2banYaml.value = result.configYaml || YAML.stringify(result.config ?? { jails: [] });
  fail2banForm.value = cloneConfig(result.config ?? { jails: [] });
}

function cloneConfig(config: Fail2BanConfigDto): Fail2BanConfigDto {
  return { jails: (config.jails ?? []).map((jail) => ({ ...jail, ignoreip: [...(jail.ignoreip ?? [])], options: { ...(jail.options ?? {}) } })) };
}

function presetLabel(value?: string) {
  const preset = presetJails.find((item) => item.value === value);
  return preset ? t(preset.titleKey) : t('firewallPage.fail2banPresetCustom');
}

function newJail(presetValue = 'ssh'): Fail2BanJailDto {
  const preset = presetJails.find((item) => item.value === presetValue) ?? presetJails[0];
  return cloneConfig({ jails: [preset.jail] }).jails[0];
}

function applyPreset(jail: Fail2BanJailDto, presetValue: string) {
  const next = newJail(presetValue);
  const currentIgnore = jail.ignoreip?.length ? [...jail.ignoreip] : next.ignoreip;
  Object.assign(jail, next, { ignoreip: currentIgnore, options: { ...(next.options ?? {}) } });
  ensureUniqueJailName(jail);
  syncRawFromRules();
}

function ensureUniqueJailName(jail: Fail2BanJailDto) {
  const names = new Set(fail2banForm.value.jails.filter((item) => item !== jail).map((item) => item.name));
  const base = jail.name || 'custom';
  let index = 2;
  while (names.has(jail.name)) {
    jail.name = `${base}-${index}`;
    index += 1;
  }
}

function addJail(preset = 'ssh') {
  const jail = newJail(preset);
  fail2banForm.value = { jails: [...fail2banForm.value.jails, jail] };
  ensureUniqueJailName(jail);
  syncRawFromRules();
}

function removeJail(index: number) {
  fail2banForm.value = { jails: fail2banForm.value.jails.filter((_, itemIndex) => itemIndex !== index) };
  syncRawFromRules();
}

function addOption(jail: Fail2BanJailDto) {
  const options = { ...(jail.options ?? {}) };
  let key = 'mode';
  let index = 2;
  while (Object.prototype.hasOwnProperty.call(options, key)) {
    key = `option${index}`;
    index += 1;
  }
  options[key] = '';
  jail.options = options;
  syncRawFromRules();
}

function removeOption(jail: Fail2BanJailDto, key: string) {
  const options = { ...(jail.options ?? {}) };
  delete options[key];
  jail.options = options;
  syncRawFromRules();
}

function renameOption(jail: Fail2BanJailDto, oldKey: string, nextKey: string) {
  nextKey = nextKey.trim();
  if (!nextKey || nextKey === oldKey) return;
  const options = { ...(jail.options ?? {}) };
  const value = options[oldKey] ?? '';
  delete options[oldKey];
  options[nextKey] = value;
  jail.options = options;
  syncRawFromRules();
}

function optionEntries(jail: Fail2BanJailDto) {
  return Object.entries(jail.options ?? {});
}

function parseRawConfig() {
  fail2banRawError.value = '';
  try {
    const parsed = YAML.parse(fail2banYaml.value || 'jails: []') as Fail2BanConfigDto;
    if (!parsed || !Array.isArray(parsed.jails)) throw new Error(t('firewallPage.fail2banRawInvalidShape'));
    fail2banForm.value = cloneConfig(parsed);
    return parsed;
  } catch (err) {
    fail2banRawError.value = err instanceof Error ? err.message : t('firewallPage.fail2banRawInvalid');
    return null;
  }
}

function syncRawFromRules() {
  if (fail2banTab.value !== 'rules') return;
  fail2banYaml.value = YAML.stringify(fail2banForm.value);
}

function currentConfig() {
  const config = fail2banTab.value === 'raw' ? parseRawConfig() : fail2banForm.value;
  if (!config) return null;
  if (fail2banTab.value === 'rules') syncRawFromRules();
  return config;
}

function onTabUpdate(value: unknown) {
  if (value === 'raw') syncRawFromRules();
  if (value === 'rules') parseRawConfig();
}

async function saveFail2BanDraft() {
  if (!serverId.value || !canManageFail2Ban.value || !currentConfig()) return;
  savingFail2Ban.value = true;
  try {
    const result = await serversApi.saveFail2Ban(serverId.value, { configYaml: fail2banYaml.value });
    applyFail2BanState(result);
    showMessage(t('firewallPage.fail2banSaveStarted'));
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('firewallPage.fail2banSaveFailed'), 'error');
  } finally {
    savingFail2Ban.value = false;
  }
}

async function runPrimaryAction(confirmTakeover = false) {
  if (!serverId.value || !canManageFail2Ban.value || !currentConfig()) return;
  if (fail2banState.value?.installed && !fail2banState.value.managed && !confirmTakeover) {
    takeoverDialog.value = true;
    return;
  }
  applyingFail2Ban.value = true;
  takeoverDialog.value = false;
  try {
    const result = await serversApi.enableFail2Ban(serverId.value, { configYaml: fail2banYaml.value, confirmTakeover });
    showMessage(t('firewallPage.fail2banInstallStarted'), 'success', result.taskId);
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('firewallPage.fail2banInstallFailed'), 'error');
  } finally {
    applyingFail2Ban.value = false;
  }
}

async function releaseFail2Ban() {
  if (!serverId.value || !canManageFail2Ban.value) return;
  releasingFail2Ban.value = true;
  releaseDialog.value = false;
  try {
    const result = await serversApi.releaseFail2Ban(serverId.value);
    showMessage(t('firewallPage.fail2banReleaseStarted'), 'success', result.taskId);
  } catch (err) {
    showMessage(err instanceof Error ? err.message : t('firewallPage.fail2banReleaseFailed'), 'error');
  } finally {
    releasingFail2Ban.value = false;
  }
}

watch(serverId, () => {
  void loadState();
});

watch(fail2banTab, onTabUpdate);

onMounted(async () => {
  await loadServers();
});
</script>

<template>
  <div class="page-shell">
    <v-alert v-if="error" type="warning" variant="tonal" class="mb-4">{{ error }}</v-alert>

    <div class="security-workspace">
      <ServerSelector v-model="serverId" :servers="servers" :loading="loadingServers" />

      <v-card v-if="selectedServer" variant="outlined" class="security-panel" :loading="loadingState">
        <div class="panel-header">
          <div class="min-width-0">
            <div class="d-flex align-center ga-2 mb-1">
              <v-icon color="primary">mdi-shield-account-outline</v-icon>
              <div class="text-h6 font-weight-bold text-truncate">{{ selectedServer.name }}</div>
            </div>
            <div class="text-body-2 text-medium-emphasis">{{ selectedServer.host }}:{{ selectedServer.port }}</div>
          </div>
          <div class="panel-actions">
            <v-chip :color="fail2banStatusColor()" variant="tonal" label>{{ fail2banStatusLabel() }}</v-chip>
            <v-btn icon="mdi-refresh" variant="outlined" size="small" :aria-label="t('common.refresh')" @click="loadState" />
          </div>
        </div>

        <div class="security-panel-body">
          <PageLoadingState v-if="loadingState && !fail2banState" min-height="280px" />

          <template v-else>
            <section class="security-section">
              <div class="section-heading">
                <div>
                  <div class="text-subtitle-1 font-weight-bold">{{ t('firewallPage.fail2banSection') }}</div>
                  <div class="text-caption text-medium-emphasis">{{ t('firewallPage.fail2banSectionSubtitle') }}</div>
                </div>
                <div class="panel-actions">
                  <v-btn variant="outlined" prepend-icon="mdi-content-save" class="text-none" :loading="savingFail2Ban" :disabled="!canManageFail2Ban" @click="saveFail2BanDraft">
                    {{ t('firewallPage.fail2banSaveDraft') }}
                  </v-btn>
                  <v-btn color="primary" variant="flat" prepend-icon="mdi-shield-check" class="text-none" :loading="applyingFail2Ban" :disabled="!canManageFail2Ban" @click="runPrimaryAction(false)">
                    {{ primaryActionLabel() }}
                  </v-btn>
                  <v-btn v-if="isManaged" color="error" variant="text" prepend-icon="mdi-link-off" class="text-none" :loading="releasingFail2Ban" :disabled="!canManageFail2Ban" @click="releaseDialog = true">
                    {{ t('firewallPage.fail2banRelease') }}
                  </v-btn>
                </div>
              </div>

              <v-alert v-if="fail2banState && !fail2banState.managed" type="info" variant="tonal" density="compact" class="mb-3">
                {{ t('firewallPage.fail2banUnmanagedHint') }}
              </v-alert>

              <div class="status-grid">
                <div>
                  <span>{{ t('firewallPage.status') }}</span>
                  <strong>{{ fail2banStatusLabel() }}</strong>
                </div>
                <div>
                  <span>{{ t('firewallPage.fail2banManaged') }}</span>
                  <strong>{{ fail2banState?.managed ? t('firewallPage.fail2banManaged') : t('firewallPage.fail2banUnmanagedShort') }}</strong>
                </div>
                <div>
                  <span>{{ t('firewallPage.fail2banPanelConfig') }}</span>
                  <strong>{{ fail2banState?.panelConfigPresent ? t('firewallPage.fail2banPanelConfigPresent') : t('firewallPage.fail2banPanelConfigMissing') }}</strong>
                </div>
                <div>
                  <span>{{ t('firewallPage.fail2banRunningJails') }}</span>
                  <strong>{{ fail2banState?.jails?.length ?? 0 }}</strong>
                </div>
                <div>
                  <span>{{ t('firewallPage.fail2banManagedJails') }}</span>
                  <strong>{{ fail2banForm.jails.length }}</strong>
                </div>
              </div>

              <v-tabs v-model="fail2banTab" density="compact" class="mt-4">
                <v-tab value="rules">{{ t('firewallPage.fail2banUiMode') }}</v-tab>
                <v-tab value="raw">{{ t('firewallPage.fail2banRawMode') }}</v-tab>
              </v-tabs>

              <v-window v-model="fail2banTab" class="fail2ban-editor">
                <v-window-item value="rules">
                  <div class="rules-heading">
                    <div>
                      <div class="text-subtitle-2 font-weight-bold">{{ t('firewallPage.fail2banRulesTitle') }}</div>
                      <div class="text-caption text-medium-emphasis">{{ t('firewallPage.fail2banRulesSubtitle') }}</div>
                    </div>
                    <v-menu>
                      <template #activator="{ props }">
                        <v-btn v-bind="props" variant="outlined" prepend-icon="mdi-plus" class="text-none">
                          {{ t('firewallPage.fail2banAddJail') }}
                        </v-btn>
                      </template>
                      <v-list density="compact">
                        <v-list-item v-for="preset in presetJails" :key="preset.value" @click="addJail(preset.value)">
                          <v-list-item-title>{{ t(preset.titleKey) }}</v-list-item-title>
                        </v-list-item>
                      </v-list>
                    </v-menu>
                  </div>

                  <div v-if="!fail2banForm.jails.length" class="empty-rules">
                    <v-icon color="medium-emphasis">mdi-shield-plus-outline</v-icon>
                    <div class="text-body-2 text-medium-emphasis">{{ t('firewallPage.fail2banNoRules') }}</div>
                  </div>

                  <div v-else class="rule-list">
                    <div v-for="(jail, index) in fail2banForm.jails" :key="`${jail.name}-${index}`" class="rule-panel">
                      <div class="rule-header">
                        <div class="min-width-0">
                          <div class="d-flex align-center ga-2">
                            <v-switch v-model="jail.enabled" color="primary" density="compact" hide-details @update:model-value="syncRawFromRules" />
                            <div class="text-subtitle-2 font-weight-bold text-truncate">{{ jail.name || t('firewallPage.fail2banRuleName') }}</div>
                            <v-chip size="small" variant="tonal" label>{{ presetLabel(jail.preset) }}</v-chip>
                          </div>
                        </div>
                        <v-btn icon="mdi-delete" color="error" variant="text" size="small" :aria-label="t('common.delete')" @click="removeJail(index)" />
                      </div>

                      <div class="jail-grid">
                        <v-select v-model="jail.preset" :items="presetOptions" :label="t('firewallPage.fail2banPreset')" item-title="title" item-value="value" variant="outlined" density="compact" @update:model-value="applyPreset(jail, String($event))" />
                        <v-text-field v-model="jail.name" :label="t('firewallPage.fail2banRuleName')" variant="outlined" density="compact" @update:model-value="syncRawFromRules" />
                        <v-text-field v-model.number="jail.maxretry" type="number" :label="t('firewallPage.fail2banMaxRetry')" variant="outlined" density="compact" @update:model-value="syncRawFromRules" />
                        <v-text-field v-model="jail.findtime" :label="t('firewallPage.fail2banFindTime')" variant="outlined" density="compact" @update:model-value="syncRawFromRules" />
                        <v-text-field v-model="jail.bantime" :label="t('firewallPage.fail2banBanTime')" variant="outlined" density="compact" @update:model-value="syncRawFromRules" />
                        <v-text-field v-model="jail.logpath" :label="t('firewallPage.fail2banLogPath')" variant="outlined" density="compact" @update:model-value="syncRawFromRules" />
                        <v-combobox v-model="jail.ignoreip" :label="t('firewallPage.fail2banIgnoreIp')" multiple chips closable-chips variant="outlined" density="compact" class="span-all" @update:model-value="syncRawFromRules" />
                      </div>

                      <v-expansion-panels variant="accordion" class="advanced-panels">
                        <v-expansion-panel>
                          <v-expansion-panel-title>{{ t('firewallPage.fail2banOptions') }}</v-expansion-panel-title>
                          <v-expansion-panel-text>
                            <div class="jail-grid">
                              <v-text-field v-model="jail.filter" :label="t('firewallPage.fail2banFilter')" variant="outlined" density="compact" @update:model-value="syncRawFromRules" />
                              <v-text-field v-model="jail.backend" :label="t('firewallPage.fail2banBackend')" variant="outlined" density="compact" @update:model-value="syncRawFromRules" />
                              <v-text-field v-model="jail.port" :label="t('firewallPage.port')" variant="outlined" density="compact" @update:model-value="syncRawFromRules" />
                              <v-text-field v-model="jail.protocol" :label="t('firewallPage.protocol')" variant="outlined" density="compact" @update:model-value="syncRawFromRules" />
                              <v-text-field v-model="jail.action" :label="t('firewallPage.fail2banAction')" variant="outlined" density="compact" class="span-all" @update:model-value="syncRawFromRules" />
                            </div>

                            <div class="option-list">
                              <div class="option-list-header">
                                <span>{{ t('firewallPage.fail2banOptions') }}</span>
                                <v-btn size="small" variant="outlined" prepend-icon="mdi-plus" class="text-none" @click="addOption(jail)">{{ t('firewallPage.fail2banAddOption') }}</v-btn>
                              </div>
                              <div v-for="[key, value] in optionEntries(jail)" :key="key" class="option-row">
                                <v-text-field :model-value="key" :label="t('common.name')" density="compact" variant="outlined" hide-details @update:model-value="renameOption(jail, key, String($event))" />
                                <v-text-field :model-value="value" :label="t('common.value')" density="compact" variant="outlined" hide-details @update:model-value="jail.options = { ...(jail.options ?? {}), [key]: String($event) }; syncRawFromRules()" />
                                <v-btn icon="mdi-close" color="error" variant="text" size="small" :aria-label="t('common.delete')" @click="removeOption(jail, key)" />
                              </div>
                            </div>
                          </v-expansion-panel-text>
                        </v-expansion-panel>
                      </v-expansion-panels>
                    </div>
                  </div>
                </v-window-item>

                <v-window-item value="raw">
                  <v-alert v-if="fail2banRawError" type="error" variant="tonal" density="compact" class="mb-3">{{ fail2banRawError }}</v-alert>
                  <v-textarea v-model="fail2banYaml" :label="t('firewallPage.fail2banRawYaml')" variant="outlined" density="compact" rows="18" class="raw-yaml" spellcheck="false" @blur="parseRawConfig" />
                </v-window-item>
              </v-window>
            </section>
          </template>
        </div>
      </v-card>

      <v-card v-else variant="outlined" class="empty-panel">
        <v-icon size="32" color="medium-emphasis">mdi-shield-search</v-icon>
        <div class="text-body-2 text-medium-emphasis">{{ t('firewallPage.selectServer') }}</div>
      </v-card>
    </div>

    <v-dialog v-model="takeoverDialog" max-width="520">
      <v-card>
        <v-card-title>{{ t('firewallPage.fail2banTakeoverTitle') }}</v-card-title>
        <v-card-text>{{ t('firewallPage.fail2banTakeoverConfirm') }}</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="takeoverDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="primary" variant="flat" :loading="applyingFail2Ban" @click="runPrimaryAction(true)">{{ t('firewallPage.fail2banTakeover') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-dialog v-model="releaseDialog" max-width="520">
      <v-card>
        <v-card-title>{{ t('firewallPage.fail2banReleaseTitle') }}</v-card-title>
        <v-card-text>{{ t('firewallPage.fail2banReleaseConfirm') }}</v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn variant="text" @click="releaseDialog = false">{{ t('common.cancel') }}</v-btn>
          <v-btn color="error" variant="flat" :loading="releasingFail2Ban" @click="releaseFail2Ban">{{ t('firewallPage.fail2banRelease') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template #actions>
        <v-btn v-if="snackbarTaskId" color="white" variant="text" :to="taskRoute()">{{ t('taskCenter.task') }}</v-btn>
        <v-btn color="white" variant="text" @click="snackbar = false">{{ t('common.close') }}</v-btn>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.security-workspace { display: grid; grid-template-columns: clamp(300px, 26vw, 340px) minmax(0, 1fr); gap: 18px; flex: 1 1 auto; min-height: 0; align-items: stretch; }
.security-panel { display: flex; flex-direction: column; min-width: 0; min-height: 0; padding: 16px; overflow: hidden; }
.panel-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; margin-bottom: 16px; }
.panel-actions { display: flex; align-items: center; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }
.security-panel-body { display: flex; flex: 1 1 auto; flex-direction: column; min-height: 0; overflow: auto; padding-right: 2px; }
.security-section { display: flex; flex-direction: column; min-width: 0; }
.section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
.status-grid { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 10px; }
.status-grid > div { display: flex; align-items: center; justify-content: space-between; gap: 10px; min-width: 0; padding: 10px 12px; border: 1px solid var(--lp-border); border-radius: 8px; }
.status-grid span { color: var(--lp-text-muted); font-size: 0.78rem; }
.status-grid strong { min-width: 0; overflow-wrap: anywhere; text-align: right; font-size: 0.86rem; }
.fail2ban-editor { margin-top: 12px; }
.rules-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
.rule-list { display: flex; flex-direction: column; gap: 12px; }
.rule-panel { border: 1px solid var(--lp-border); border-radius: 8px; padding: 14px; }
.rule-header { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 10px; }
.jail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px 12px; }
.span-all { grid-column: 1 / -1; }
.advanced-panels { margin-top: 8px; }
.option-list { display: flex; flex-direction: column; gap: 8px; margin-top: 10px; }
.option-list-header { display: flex; align-items: center; justify-content: space-between; gap: 10px; color: var(--lp-text-muted); font-size: 0.78rem; font-weight: 650; }
.option-row { display: grid; grid-template-columns: minmax(120px, 0.34fr) minmax(0, 1fr) auto; gap: 8px; align-items: center; }
.raw-yaml { font-family: ui-monospace, SFMono-Regular, Consolas, "Liberation Mono", monospace; }
.empty-panel, .empty-rules { min-height: 220px; display: grid; place-items: center; align-content: center; gap: 10px; padding: 32px; text-align: center; border: 1px dashed var(--lp-border); border-radius: 8px; }
.empty-panel { min-height: 340px; border-style: solid; }
.min-width-0 { min-width: 0; }
@media (max-width: 1180px) {
  .status-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}
@media (max-width: 1080px) {
  .security-workspace { grid-template-columns: 1fr; }
}
@media (max-width: 760px) {
  .panel-header, .section-heading, .rules-heading { flex-direction: column; align-items: stretch; }
  .status-grid, .jail-grid, .option-row { grid-template-columns: 1fr; }
  .security-workspace { flex: none; min-height: auto; }
  .security-panel { overflow: visible; }
  .security-panel-body { flex: none; overflow: visible; }
}
</style>
