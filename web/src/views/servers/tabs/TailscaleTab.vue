<script setup lang="ts">
import { ref } from 'vue';
import { RefreshCcw } from '@lucide/vue';
import { serversApi } from '@/api/servers';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import Switch from '@/components/ui/Switch.vue';
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { useI18n } from '@/i18n';
import type { ServerDto } from '@/types/servers';
import { formatDateTime } from '@/utils/datetime';
import { tailscaleStatusKey, tailscaleTone, tailscaleTrait } from '../model';

const props = defineProps<{ server: ServerDto }>();
const emit = defineEmits<{ refresh: [] }>();
const { t } = useI18n();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();

const applying = ref(false);

async function apply() {
  if (!props.server.tailscaleEnabled || applying.value) return;
  applying.value = true;
  try {
    const accepted = await serversApi.applyTailscale(props.server.id);
    notifySuccess(t('serversPage.tailscale.applyAccepted', { taskId: accepted.taskId }), accepted);
    emit('refresh');
  } catch (err) {
    notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err);
  } finally {
    applying.value = false;
  }
}
</script>

<template>
  <div class="h-full min-h-0 overflow-y-auto pr-1">
    <div class="grid content-start gap-4 2xl:grid-cols-[minmax(0,1fr)_320px]">
      <section class="rounded-2xl border border-border bg-muted p-4">
        <div class="flex min-w-0 flex-wrap items-center justify-between gap-2">
          <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('serversPage.tailscale.title') }}</h3>
          <Badge v-if="server.tailscaleEnabled" :tone="tailscaleTone(server)">{{ t(tailscaleStatusKey(server)) }}</Badge>
        </div>
        <p v-if="!server.tailscaleEnabled" class="mt-2 text-sm text-muted-foreground">{{ t('serversPage.tailscale.disabledHint') }}</p>
        <template v-else>
          <div class="mt-3 grid gap-2 text-sm">
            <label class="flex items-center justify-between gap-3">{{ t('serversPage.tailscale.enabled') }}<Switch :model-value="server.tailscaleEnabled" disabled :label="t('serversPage.tailscale.enabled')" /></label>
            <label class="flex items-center justify-between gap-3">{{ t('serversPage.tailscale.preferAgent') }}<Switch :model-value="server.tailscalePreferAgent" disabled :label="t('serversPage.tailscale.preferAgent')" /></label>
            <label class="flex items-center justify-between gap-3">{{ t('serversPage.tailscale.preferInterconnect') }}<Switch :model-value="server.tailscalePreferInterconnect" disabled :label="t('serversPage.tailscale.preferInterconnect')" /></label>
          </div>
          <dl class="mt-3 grid grid-cols-2 gap-3 text-sm max-md:grid-cols-1">
            <div><dt>{{ t('serversPage.tailscale.hostname') }}</dt><dd>{{ tailscaleTrait(server, 'hostname') || t('common.notAvailable') }}</dd></div>
            <div><dt>{{ t('serversPage.tailscale.updatedAt') }}</dt><dd>{{ formatDateTime(tailscaleTrait(server, 'updatedAt'), t('common.never')) }}</dd></div>
            <div><dt>{{ t('serversPage.tailscale.ipv4') }}</dt><dd>{{ tailscaleTrait(server, 'ipv4') || t('common.notAvailable') }}</dd></div>
            <div><dt>{{ t('serversPage.tailscale.ipv6') }}</dt><dd>{{ tailscaleTrait(server, 'ipv6') || t('common.notAvailable') }}</dd></div>
          </dl>
          <p v-if="tailscaleTrait(server, 'lastError')" class="mt-3 grid gap-1 rounded-xl border border-danger-border bg-danger-bg p-3 text-sm text-danger">
            <strong>{{ t('serversPage.tailscale.lastError') }}</strong>
            <span class="break-words">{{ tailscaleTrait(server, 'lastError') }}</span>
          </p>
        </template>
      </section>

      <aside class="grid content-start gap-3">
        <section class="rounded-2xl border border-border bg-muted p-4">
          <h3 class="m-0 text-sm font-semibold text-foreground">{{ t('serversPage.tailscale.apply') }}</h3>
          <p class="m-0 mt-2 text-xs text-muted-foreground">{{ t('serversPage.tailscale.applyHint') }}</p>
          <Button class="mt-3 w-full" :disabled="!server.tailscaleEnabled" :loading="applying" @click="apply"><RefreshCcw />{{ t('serversPage.tailscale.apply') }}</Button>
        </section>
      </aside>
    </div>
  </div>
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
