<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { serversApi, type CredentialInput, type ServerInput } from '@/api/servers';
import { composeApi } from '@/api/compose';
import type { CredentialDto, ServerDto } from '@/types/api';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';

const servers = ref<ServerDto[]>([]);
const credentials = ref<CredentialDto[]>([]);
const loading = ref(false);
const error = ref('');
const serverDialog = ref(false);
const credentialDialog = ref(false);
const variablesDialog = ref(false);
const editing = ref<ServerDto | null>(null);
const editingCredential = ref<CredentialDto | null>(null);
const variableServer = ref<ServerDto | null>(null);
const activeTaskId = ref('');
const activeTaskServerName = ref('');
const activeTab = ref('servers');
const serverVariablesJson = ref('{}');

// Snackbar notification state
const snackbar = ref(false);
const snackbarText = ref('');
const snackbarColor = ref('success');

function showMessage(text: string, color = 'success') {
  snackbarText.value = text;
  snackbarColor.value = color;
  snackbar.value = true;
}

// Confirmation Dialog State
const confirmDialog = ref(false);
const confirmTitle = ref('Confirm action');
const confirmMessage = ref('');
const confirmAction = ref<(() => Promise<void>) | null>(null);

function confirm(title: string, message: string, action: () => Promise<void>) {
  confirmTitle.value = title;
  confirmMessage.value = message;
  confirmAction.value = action;
  confirmDialog.value = true;
}

async function executeConfirm() {
  if (confirmAction.value) {
    try {
      await confirmAction.value();
    } catch (err) {
      showMessage(err instanceof Error ? err.message : 'Action failed', 'error');
    }
  }
  confirmDialog.value = false;
}

const serverForm = reactive<ServerInput>({
  name: '',
  host: '',
  port: 22,
  sshUsername: '',
  credentialId: null,
  labels: [],
  notes: '',
});

const credentialForm = reactive<CredentialInput>({
  name: '',
  type: 'password',
  username: '',
  password: '',
  privateKey: '',
  passphrase: '',
});

const credentialOptions = computed(() =>
  credentials.value.map((credential) => ({
    label: `${credential.name} (${credential.username}, ${credential.type})`,
    value: credential.id,
  })),
);

function credentialById(id?: string | null) {
  return credentials.value.find((credential) => credential.id === id);
}

function resetServerForm(server?: ServerDto) {
  editing.value = server ?? null;
  Object.assign(serverForm, {
    name: server?.name ?? '',
    host: server?.host ?? '',
    port: server?.port ?? 22,
    sshUsername: server?.sshUsername ?? '',
    credentialId: server?.credentialId ?? null,
    labels: server?.labels ?? [],
    notes: server?.notes ?? '',
  });
  serverDialog.value = true;
}

function resetCredentialForm() {
  editingCredential.value = null;
  Object.assign(credentialForm, {
    name: '',
    type: 'password',
    username: '',
    password: '',
    privateKey: '',
    passphrase: '',
  });
  credentialDialog.value = true;
}

function editCredential(credential: CredentialDto) {
  editingCredential.value = credential;
  Object.assign(credentialForm, {
    name: credential.name,
    type: credential.type,
    username: credential.username,
    password: '',
    privateKey: '',
    passphrase: '',
  });
  credentialDialog.value = true;
}

async function load() {
  loading.value = true;
  try {
    const [serverRows, credentialRows] = await Promise.all([
      serversApi.listServers(),
      serversApi.listCredentials(),
    ]);
    servers.value = serverRows;
    credentials.value = credentialRows;
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load servers';
  } finally {
    loading.value = false;
  }
}

async function loadServerVariables(serverId: string) {
  try {
    const variables = await composeApi.getServerVariables(serverId);
    serverVariablesJson.value = JSON.stringify(variables ?? {}, null, 2);
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Unable to load server variables', 'error');
  }
}

async function openVariables(server: ServerDto) {
  variableServer.value = server;
  serverVariablesJson.value = '{}';
  variablesDialog.value = true;
  await loadServerVariables(server.id);
}

function parseServerVariables() {
  try {
    return JSON.parse(serverVariablesJson.value || '{}') as Record<string, unknown>;
  } catch {
    throw new Error('Server variables must be valid JSON');
  }
}

async function saveServer() {
  try {
    if (editing.value) {
      await serversApi.updateServer(editing.value.id, serverForm);
      showMessage('Server updated successfully');
    } else {
      await serversApi.createServer(serverForm);
      showMessage('Server created successfully');
    }
    serverDialog.value = false;
    await load();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Failed to save server', 'error');
  }
}

async function saveServerVariables() {
  if (!variableServer.value) return;
  try {
    await composeApi.updateServerVariables(variableServer.value.id, parseServerVariables());
    variablesDialog.value = false;
    showMessage('Server variables saved');
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Failed to save server variables', 'error');
  }
}

async function saveCredential() {
  try {
    const input = { ...credentialForm };
    if (input.type === 'password') {
      delete input.privateKey;
      delete input.passphrase;
    } else {
      delete input.password;
    }
    const credential = editingCredential.value
      ? await serversApi.updateCredential(editingCredential.value.id, input)
      : await serversApi.createCredential(input);
    serverForm.credentialId = credential.id;
    credentialDialog.value = false;
    showMessage(editingCredential.value ? 'Credential updated' : 'Credential created');
    await load();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Failed to save credential', 'error');
  }
}

async function deleteServer(server: ServerDto) {
  confirm('Confirm delete', `Delete server ${server.name}?`, async () => {
    await serversApi.deleteServer(server.id);
    showMessage('Server deleted');
    await load();
  });
}

async function deleteCredential(credential: CredentialDto) {
  confirm('Confirm delete', `Delete credential ${credential.name}?`, async () => {
    await serversApi.deleteCredential(credential.id);
    showMessage('Credential deleted');
    await load();
  });
}

async function testServer(server: ServerDto) {
  try {
    const result = await serversApi.testConnection(server.id);
    activeTaskId.value = result.taskId;
    activeTaskServerName.value = server.name;
    showMessage('Connectivity test started');
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Connection test failed', 'error');
  }
}

async function handleTaskFinished() {
  await load();
  showMessage('Connectivity test finished');
}

onMounted(load);
</script>

<template>
  <div>
    <div class="d-flex justify-space-between align-center mb-6">
      <div>
        <h1 class="text-h4 font-weight-bold">Servers</h1>
        <p class="text-subtitle-1 text-medium-emphasis">Register SSH targets, attach credentials, and validate distro readiness.</p>
      </div>
      <div class="d-flex" style="gap: 12px;">
        <v-btn
          prepend-icon="mdi-refresh"
          :loading="loading"
          variant="outlined"
          @click="load"
          class="text-none font-weight-bold"
        >
          Refresh
        </v-btn>
        <v-btn
          v-if="activeTab === 'credentials'"
          color="primary"
          prepend-icon="mdi-plus"
          @click="resetCredentialForm()"
          class="text-none font-weight-bold"
        >
          Add Credential
        </v-btn>
        <v-btn
          v-else
          color="primary"
          prepend-icon="mdi-plus"
          @click="resetServerForm()"
          class="text-none font-weight-bold"
        >
          Add Server
        </v-btn>
      </div>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-4">{{ error }}</v-alert>

    <v-card variant="outlined" class="mb-6">
      <v-tabs v-model="activeTab" color="primary" border-bottom>
        <v-tab value="servers" class="text-none font-weight-bold">Servers</v-tab>
        <v-tab value="credentials" class="text-none font-weight-bold">Credentials</v-tab>
      </v-tabs>

      <v-window v-model="activeTab">
        <v-window-item value="servers">
          <v-table class="text-left" style="background: transparent;">
            <thead>
              <tr>
                <th class="font-weight-bold">Name</th>
                <th class="font-weight-bold">Host</th>
                <th class="font-weight-bold">SSH User</th>
                <th class="font-weight-bold">Distro</th>
                <th class="font-weight-bold">Sudo</th>
                <th class="font-weight-bold text-right" style="width: 360px;">Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="servers.length === 0">
                <td colspan="6" class="text-center py-6 text-grey-darken-1">No servers registered</td>
              </tr>
              <tr v-for="row in servers" :key="row.id">
                <td class="font-weight-bold">{{ row.name }}</td>
                <td class="font-tabular">{{ row.host }}:{{ row.port }}</td>
                <td>
                  <div>{{ row.sshUsername || credentialById(row.credentialId)?.username || 'credential' }}</div>
                  <div class="text-caption text-grey-darken-1">
                    {{ row.sshUsername ? 'override' : 'from credential' }}
                  </div>
                </td>
                <td>
                  <v-chip :color="row.os?.supported ? 'success' : 'warning'" size="small" label>
                    {{ row.os?.prettyName || 'unknown' }}
                  </v-chip>
                </td>
                <td>
                  <div class="d-flex align-center">
                    <span :class="['status-dot mr-2', row.sudo?.passwordless ? 'ok' : 'warn']"></span>
                    {{ row.sudo?.passwordless ? 'passwordless' : 'unchecked' }}
                  </div>
                </td>
                <td class="text-right">
                  <div class="d-flex justify-end" style="gap: 6px;">
                    <v-btn size="small" variant="outlined" prepend-icon="mdi-pencil" @click="resetServerForm(row)">Edit</v-btn>
                    <v-btn size="small" variant="outlined" prepend-icon="mdi-variable" @click="openVariables(row)">Variables</v-btn>
                    <v-btn size="small" color="error" variant="outlined" prepend-icon="mdi-delete" @click="deleteServer(row)">Delete</v-btn>
                  </div>
                </td>
              </tr>
            </tbody>
          </v-table>
        </v-window-item>

        <v-window-item value="credentials">
          <v-table class="text-left" style="background: transparent;">
            <thead>
              <tr>
                <th class="font-weight-bold">Name</th>
                <th class="font-weight-bold">Username</th>
                <th class="font-weight-bold">Type</th>
                <th class="font-weight-bold text-right" style="width: 180px;">Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="credentials.length === 0">
                <td colspan="4" class="text-center py-6 text-grey-darken-1">No credentials registered</td>
              </tr>
              <tr v-for="row in credentials" :key="row.id">
                <td class="font-weight-bold">{{ row.name }}</td>
                <td>{{ row.username }}</td>
                <td>
                  <v-chip size="small" label color="grey-darken-1">{{ row.type }}</v-chip>
                </td>
                <td class="text-right">
                  <div class="d-flex justify-end" style="gap: 6px;">
                    <v-btn size="small" variant="outlined" prepend-icon="mdi-pencil" @click="editCredential(row)">Edit</v-btn>
                    <v-btn size="small" color="error" variant="outlined" prepend-icon="mdi-delete" @click="deleteCredential(row)">Delete</v-btn>
                  </div>
                </td>
              </tr>
            </tbody>
          </v-table>
        </v-window-item>
      </v-window>
    </v-card>

    <v-card v-slot:default v-if="activeTaskId" class="mb-6 pa-4" variant="outlined">
      <v-card-title class="px-0 pt-0 text-subtitle-1 font-weight-bold">Connectivity Test</v-card-title>
      <v-card-text class="px-0 pb-0">
        <TaskLogPanel
          :task-id="activeTaskId"
          :server-name="activeTaskServerName"
          compact
          @finished="handleTaskFinished"
        />
      </v-card-text>
    </v-card>

    <!-- Add/Edit Server Dialog -->
    <v-dialog v-model="serverDialog" max-width="560px" scrollable>
      <v-card>
        <v-card-title class="bg-surface-variant py-3 font-weight-bold">
          {{ editing ? 'Edit server' : 'Add server' }}
        </v-card-title>
        <v-divider />
        <v-card-text class="pa-4">
          <v-form @submit.prevent="saveServer">
            <v-text-field v-model="serverForm.name" label="Name" variant="outlined" density="comfortable" class="mb-3" />
            <v-text-field v-model="serverForm.host" label="Host" variant="outlined" density="comfortable" class="mb-3" />
            <v-text-field v-model.number="serverForm.port" type="number" label="Port" variant="outlined" density="comfortable" class="mb-3" />

            <v-select
              v-model="serverForm.credentialId"
              :items="credentialOptions"
              item-title="label"
              item-value="value"
              label="Select credential"
              placeholder="Select credential"
              variant="outlined"
              density="comfortable"
              class="mb-3"
              clearable
            />

            <v-text-field
              v-model="serverForm.sshUsername"
              label="SSH username override"
              placeholder="Optional. Leave empty to use credential username"
              variant="outlined"
              density="comfortable"
              class="mb-3"
            />

            <v-combobox
              v-model="serverForm.labels"
              label="Labels"
              multiple
              chips
              closable-chips
              variant="outlined"
              density="comfortable"
              class="mb-3"
              placeholder="Type label and hit Enter"
            />

            <v-textarea v-model="serverForm.notes" label="Notes" variant="outlined" density="comfortable" rows="3" />
          </v-form>
        </v-card-text>
        <v-divider />
        <v-card-actions class="pa-3 bg-surface-variant">
          <v-spacer />
          <v-btn variant="outlined" class="text-none font-weight-bold" @click="serverDialog = false">Cancel</v-btn>
          <v-btn color="primary" variant="flat" class="text-none font-weight-bold" @click="saveServer">Save</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Add/Edit Credential Dialog -->
    <v-dialog v-model="credentialDialog" max-width="620px" scrollable>
      <v-card>
        <v-card-title class="bg-surface-variant py-3 font-weight-bold">
          {{ editingCredential ? 'Edit credential' : 'Create credential' }}
        </v-card-title>
        <v-divider />
        <v-card-text class="pa-4">
          <v-form @submit.prevent="saveCredential">
            <v-text-field v-model="credentialForm.name" label="Name" variant="outlined" density="comfortable" class="mb-3" />

            <div class="text-subtitle-2 mb-2">Credential Type</div>
            <v-btn-toggle v-model="credentialForm.type" mandatory color="primary" density="compact" class="mb-4">
              <v-btn value="password" class="text-none">Password</v-btn>
              <v-btn value="private_key" class="text-none">Private key</v-btn>
            </v-btn-toggle>

            <v-text-field v-model="credentialForm.username" label="Username" variant="outlined" density="comfortable" class="mb-3" />

            <v-text-field
              v-if="credentialForm.type === 'password'"
              v-model="credentialForm.password"
              type="password"
              label="Password"
              placeholder="Password"
              variant="outlined"
              density="comfortable"
              class="mb-3"
            />

            <template v-else>
              <v-textarea
                v-model="credentialForm.privateKey"
                label="Private key"
                placeholder="Paste PEM format private key here"
                variant="outlined"
                density="comfortable"
                rows="8"
                class="mb-3 font-mono"
              />
              <v-text-field
                v-model="credentialForm.passphrase"
                type="password"
                label="Passphrase"
                placeholder="Optional passphrase for private key"
                variant="outlined"
                density="comfortable"
                class="mb-3"
              />
            </template>
          </v-form>
        </v-card-text>
        <v-divider />
        <v-card-actions class="pa-3 bg-surface-variant">
          <v-spacer />
          <v-btn variant="outlined" class="text-none font-weight-bold" @click="credentialDialog = false">Cancel</v-btn>
          <v-btn color="primary" variant="flat" class="text-none font-weight-bold" @click="saveCredential">
            {{ editingCredential ? 'Save' : 'Create' }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Variables Dialog -->
    <v-dialog v-model="variablesDialog" max-width="620px" scrollable>
      <v-card>
        <v-card-title class="bg-surface-variant py-3 font-weight-bold">
          Variables: {{ variableServer?.name || '' }}
        </v-card-title>
        <v-divider />
        <v-card-text class="pa-4">
          <v-form @submit.prevent="saveServerVariables">
            <v-textarea
              v-model="serverVariablesJson"
              label="Custom variables (JSON)"
              placeholder="{}"
              variant="outlined"
              density="comfortable"
              rows="14"
              class="font-mono"
              spellcheck="false"
            />
          </v-form>
        </v-card-text>
        <v-divider />
        <v-card-actions class="pa-3 bg-surface-variant">
          <v-spacer />
          <v-btn variant="outlined" class="text-none font-weight-bold" @click="variablesDialog = false">Cancel</v-btn>
          <v-btn color="primary" variant="flat" class="text-none font-weight-bold" @click="saveServerVariables">Save Variables</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Confirmation Dialog -->
    <v-dialog v-model="confirmDialog" max-width="400px">
      <v-card>
        <v-card-title class="bg-surface-variant py-3 font-weight-bold">
          {{ confirmTitle }}
        </v-card-title>
        <v-divider />
        <v-card-text class="pa-4 text-body-1">
          {{ confirmMessage }}
        </v-card-text>
        <v-divider />
        <v-card-actions class="pa-3 bg-surface-variant">
          <v-spacer />
          <v-btn variant="outlined" class="text-none font-weight-bold" @click="confirmDialog = false">Cancel</v-btn>
          <v-btn color="error" variant="flat" class="text-none font-weight-bold" @click="executeConfirm">Confirm</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>

    <!-- Global Snackbar -->
    <v-snackbar v-model="snackbar" :color="snackbarColor" timeout="3000">
      {{ snackbarText }}
      <template v-slot:actions>
        <v-btn color="white" variant="text" @click="snackbar = false">Close</v-btn>
      </template>
    </v-snackbar>
  </div>
</template>

<style scoped>
.font-mono {
  font-family: monospace !important;
}

.status-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.status-dot.ok {
  background-color: #4caf50;
}

.status-dot.warn {
  background-color: #ff9800;
}
</style>
