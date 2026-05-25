<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import { useRoute } from 'vue-router';
import { serversApi, type CredentialInput } from '@/api/servers';
import { nomadApi } from '@/api/nomad';
import type { CredentialDto, NomadControlPlaneDto, ProjectedNomadNodeDto, ServerDto } from '@/types/api';
import TaskLogPanel from '@/components/tasks/TaskLogPanel.vue';

const route = useRoute();
const servers = ref<ServerDto[]>([]);
const credentials = ref<CredentialDto[]>([]);
const controlPlane = ref<NomadControlPlaneDto | null>(null);
const loading = ref(false);
const error = ref('');
const serverDialog = ref(false);
const credentialDialog = ref(false);
const editing = ref<ServerDto | null>(null);
const editingCredential = ref<CredentialDto | null>(null);
const activeTaskId = ref('');
const activeTaskServerName = ref('');
const activeTab = computed(() => (route.name === 'credentials' ? 'credentials' : 'servers'));
const pageTitle = computed(() => (activeTab.value === 'credentials' ? 'Credentials' : 'Servers'));
const pageSubtitle = computed(() =>
  activeTab.value === 'credentials'
    ? 'Manage reusable SSH credentials for registered servers.'
    : 'Register SSH targets, attach credentials, and auto-discover Traits.',
);

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

const serverForm = reactive({
  name: '',
  host: '',
  port: 22,
  sshUsername: '',
  credentialId: null as string | null,
  traitsRaw: [] as string[],
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

function nomadProjectionForServer(serverId: string): ProjectedNomadNodeDto | null {
  return controlPlane.value?.nodes.find((node) => node.serverId === serverId) ?? null;
}

function nomadStatusForServer(serverId: string) {
  const projection = nomadProjectionForServer(serverId);
  if (!projection) return { label: 'Not joined', color: 'grey' };
  if (projection.status === 'bootstrapping') return { label: 'Bootstrapping server', color: 'warning' };
  if (projection.status === 'joining') return { label: 'Joining client', color: 'warning' };
  if (projection.status === 'failed') return { label: 'Join failed', color: 'error' };
  if (projection.kind === 'managed') return { label: 'Managed node', color: 'primary' };
  return { label: projection.status || 'Not joined', color: 'grey' };
}

function resetServerForm(server?: ServerDto) {
  editing.value = server ?? null;

  const traitsRaw: string[] = [];
  if (server?.traits) {
    for (const [k, v] of Object.entries(server.traits)) {
      if (k.startsWith('custom.')) {
        traitsRaw.push(`${k.substring(7)}=${v}`);
      } else if (!k.startsWith('sys.')) {
        traitsRaw.push(`${k}=${v}`);
      }
    }
  }

  Object.assign(serverForm, {
    name: server?.name ?? '',
    host: server?.host ?? '',
    port: server?.port ?? 22,
    sshUsername: server?.sshUsername ?? '',
    credentialId: server?.credentialId ?? null,
    traitsRaw,
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
    const [serverRows, credentialRows, nomadState] = await Promise.all([
      serversApi.listServers(),
      serversApi.listCredentials(),
      nomadApi.controlPlane().catch(() => null),
    ]);
    servers.value = serverRows;
    credentials.value = credentialRows;
    controlPlane.value = nomadState;
    error.value = '';
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Unable to load servers';
  } finally {
    loading.value = false;
  }
}

async function saveServer() {
  try {
    const traits: Record<string, string> = {};
    for (const raw of serverForm.traitsRaw) {
      const parts = raw.split('=');
      if (parts.length >= 2) {
        let key = parts[0].trim();
        const value = parts.slice(1).join('=').trim();
        if (key) {
          if (!key.startsWith('custom.') && !key.startsWith('sys.')) {
            key = 'custom.' + key;
          }
          traits[key] = value;
        }
      }
    }

    const payload = {
      name: serverForm.name,
      host: serverForm.host,
      port: serverForm.port,
      sshUsername: serverForm.sshUsername,
      credentialId: serverForm.credentialId,
      traits,
      notes: serverForm.notes,
    };

    if (editing.value) {
      await serversApi.updateServer(editing.value.id, payload as any);
      showMessage('Server updated successfully');
    } else {
      await serversApi.createServer(payload as any);
      showMessage('Server created successfully');
    }
    serverDialog.value = false;
    await load();
  } catch (err) {
    showMessage(err instanceof Error ? err.message : 'Failed to save server', 'error');
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
        <h1 class="text-h4 font-weight-bold">{{ pageTitle }}</h1>
        <p class="text-subtitle-1 text-medium-emphasis">{{ pageSubtitle }}</p>
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

    <v-card v-if="activeTab === 'servers'" variant="outlined" class="pa-4 mb-4">
      <v-table class="text-left" style="background: transparent;">
        <thead>
          <tr>
            <th class="font-weight-bold">Name & Traits</th>
            <th class="font-weight-bold">Host</th>
            <th class="font-weight-bold">SSH User</th>
            <th class="font-weight-bold">Nomad</th>
            <th class="font-weight-bold">Distro</th>
            <th class="font-weight-bold">Sudo</th>
            <th class="font-weight-bold text-right" style="width: 380px;">Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="servers.length === 0">
            <td colspan="7" class="text-center py-6 text-grey-darken-1">No servers registered</td>
          </tr>
          <tr v-for="row in servers" :key="row.id">
            <td class="font-weight-bold py-3">
              <div>{{ row.name }}</div>

              <!-- 只读系统物理特征展示区 (CPU、物理内存、硬盘空间) -->
              <div v-if="row.traits" class="d-flex flex-wrap mt-1" style="gap: 4px;">
                <v-chip v-if="row.traits['sys.cpu_cores']" size="x-small" color="blue" variant="tonal" label>
                  {{ row.traits['sys.cpu_cores'] }} Cores
                </v-chip>
                <v-chip v-if="row.traits['sys.memory_total_mb']" size="x-small" color="purple" variant="tonal" label>
                  {{ (parseInt(row.traits['sys.memory_total_mb']) / 1024).toFixed(1) }} GB RAM
                </v-chip>
                <v-chip v-if="row.traits['sys.disk_total_gb']" size="x-small" color="orange" variant="tonal" label>
                  {{ row.traits['sys.disk_total_gb'] }} GB Disk
                </v-chip>
                <!-- 自定义特征 Traits 显示区 -->
                <template v-for="[k, v] in Object.entries(row.traits)" :key="k">
                  <v-chip v-if="k.startsWith('custom.')" size="x-small" color="success" variant="tonal" label>
                    {{ k.substring(7) }}={{ v }}
                  </v-chip>
                </template>
              </div>
            </td>
            <td class="font-tabular">{{ row.host }}:{{ row.port }}</td>
            <td>
              <div>{{ row.sshUsername || credentialById(row.credentialId)?.username || 'credential' }}</div>
              <div class="text-caption text-grey-darken-1">
                {{ row.sshUsername ? 'override' : 'from credential' }}
              </div>
            </td>
            <td>
              <v-chip :color="nomadStatusForServer(row.id).color" size="small" variant="tonal" label>
                {{ nomadStatusForServer(row.id).label }}
              </v-chip>
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
                <v-btn size="small" color="primary" variant="outlined" prepend-icon="mdi-swap-horizontal" @click="testServer(row)">Test connection</v-btn>
                <v-btn size="small" variant="outlined" prepend-icon="mdi-pencil" @click="resetServerForm(row)">Edit</v-btn>
                <v-btn size="small" color="error" variant="outlined" prepend-icon="mdi-delete" @click="deleteServer(row)">Delete</v-btn>
              </div>
            </td>
          </tr>
        </tbody>
      </v-table>
    </v-card>

    <v-card v-else variant="outlined" class="pa-4 mb-4">
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

    <v-navigation-drawer v-model="serverDialog" location="right" temporary width="560" style="z-index: 1005;">
      <div class="pa-4 fill-height d-flex flex-column">
        <div class="d-flex justify-space-between align-center mb-4">
          <div class="text-h6 font-weight-bold">{{ editing ? 'Edit server' : 'Add server' }}</div>
          <div class="d-flex align-center" style="gap: 8px;">
            <v-btn color="primary" variant="flat" size="small" class="text-none font-weight-bold" @click="saveServer">Save</v-btn>
            <v-btn icon="mdi-close" variant="text" size="small" @click="serverDialog = false" />
          </div>
        </div>
        <v-divider />
        <div class="flex-grow-1 overflow-auto mt-4">
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

            <!-- 极简又高大上的 Custom Traits 双轨组合录入组件 -->
            <v-combobox
              v-model="serverForm.traitsRaw"
              label="Custom Traits"
              multiple
              chips
              closable-chips
              variant="outlined"
              density="comfortable"
              class="mb-3"
              placeholder="Type env=prod or role=web and hit Enter"
            />

            <v-textarea v-model="serverForm.notes" label="Notes" variant="outlined" density="comfortable" rows="3" />
          </v-form>
        </div>
      </div>
    </v-navigation-drawer>

    <v-navigation-drawer v-model="credentialDialog" location="right" temporary width="620" style="z-index: 1005;">
      <div class="pa-4 fill-height d-flex flex-column">
        <div class="d-flex justify-space-between align-center mb-4">
          <div class="text-h6 font-weight-bold">{{ editingCredential ? 'Edit credential' : 'Create credential' }}</div>
          <div class="d-flex align-center" style="gap: 8px;">
            <v-btn color="primary" variant="flat" size="small" class="text-none font-weight-bold" @click="saveCredential">
              {{ editingCredential ? 'Save' : 'Create' }}
            </v-btn>
            <v-btn icon="mdi-close" variant="text" size="small" @click="credentialDialog = false" />
          </div>
        </div>
        <v-divider />
        <div class="flex-grow-1 overflow-auto mt-4">
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
        </div>
      </div>
    </v-navigation-drawer>

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
