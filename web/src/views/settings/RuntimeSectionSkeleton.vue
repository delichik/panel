<script setup lang="ts">
import { DatabaseBackup, KeyRound, Trash2, UploadCloud } from '@/icons';
import SettingsCard from '@/components/patterns/SettingsCard.vue';
import SettingsSection from '@/components/patterns/SettingsSection.vue';
import Button from '@/components/ui/Button.vue';
import Skeleton from '@/components/ui/Skeleton.vue';
import Table from '@/components/ui/Table.vue';
import { useI18n } from '@/i18n';

defineProps<{
  section: string;
  systemCertificateColumns: Array<{ key: string; label: string; align?: 'left' | 'right'; nowrap?: boolean }>;
}>();

const { t } = useI18n();
</script>

<template>
  <div aria-busy="true">
    <SettingsSection v-if="section === 'general'">
      <SettingsCard :title="t('settingsPage.containerInterval')">
        <template #control><Skeleton class="h-8 w-full sm:w-44" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.language')">
        <template #control><Skeleton class="h-8 w-full sm:w-64" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.logLevel')">
        <template #control><Skeleton class="h-8 w-full sm:w-64" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.reconcileTrace')" :description="t('settingsPage.reconcileTraceHint')">
        <template #control><Skeleton class="h-5 w-10 rounded-pill" /></template>
      </SettingsCard>
      <SettingsCard>
        <div class="flex min-w-0 flex-wrap items-center justify-between gap-3">
          <div class="min-w-0">
            <h3 class="m-0 text-sm font-medium leading-5 text-danger">{{ t('settingsPage.clearEventsLogs') }}</h3>
            <p class="m-0 text-xs leading-4 text-muted-foreground">{{ t('settingsPage.clearEventsLogsHint') }}</p>
          </div>
          <Button variant="danger" disabled><Trash2 />{{ t('settingsPage.clearEventsLogs') }}</Button>
        </div>
      </SettingsCard>
    </SettingsSection>

    <SettingsSection v-else-if="section === 'security'">
      <SettingsCard :title="t('settingsPage.tokenExpiration')">
        <template #control><Skeleton class="h-8 w-full sm:w-64" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.remoteTimeout')">
        <template #control><Skeleton class="h-8 w-full sm:w-44" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.sshAccessSuccessRetention')" :description="t('settingsPage.sshAccessRetentionHint')">
        <template #control><Skeleton class="h-8 w-full sm:w-44" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.sshAccessFailureRetention')">
        <template #control><Skeleton class="h-8 w-full sm:w-44" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.jwtSecretConfigured')" :description="t('settingsPage.jwtSecretHint')">
        <template #control>
          <div class="flex items-center gap-2">
            <Skeleton class="h-5 w-16 rounded-pill" />
            <Button disabled><KeyRound />{{ t('settingsPage.resetJwtSecret') }}</Button>
          </div>
        </template>
      </SettingsCard>
    </SettingsSection>

    <SettingsSection v-else-if="section === 'certificates'" :description="t('settingsPage.panelHttpsHint')">
      <SettingsCard :title="t('settingsPage.panelDomain')">
        <template #control><Skeleton class="h-8 w-full sm:w-64" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.panelTlsCertificate')">
        <template #control><Skeleton class="h-8 w-full sm:w-64" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.certificateEmail')">
        <template #control><Skeleton class="h-8 w-full sm:w-72" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.dnsDelay')">
        <template #control><Skeleton class="h-8 w-full sm:w-44" /></template>
      </SettingsCard>
    </SettingsSection>

    <SettingsSection v-else-if="section === 'agent'" :description="t('settingsPage.agentDownloadHint')">
      <SettingsCard :title="t('settingsPage.agentDownloadBaseUrl')" :description="t('settingsPage.agentDownloadBaseUrlHint')">
        <template #control><Skeleton class="h-8 w-full sm:w-72" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.agentTransferTimeout')" :description="t('settingsPage.agentTransferTimeoutHint')">
        <template #control><Skeleton class="h-8 w-full sm:w-44" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.agentDownloadVerifyTls')" :description="t('settingsPage.agentDownloadVerifyTlsHint')">
        <template #control><Skeleton class="h-5 w-10 rounded-pill" /></template>
      </SettingsCard>
    </SettingsSection>

    <SettingsSection v-else-if="section === 'auto-update'" :description="t('settingsPage.autoUpdateHint')">
      <SettingsCard :title="t('settingsPage.autoUpdate.appsEnabled')" :description="t('settingsPage.autoUpdate.appsEnabledHint')">
        <template #control><Skeleton class="h-5 w-10 rounded-pill" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.autoUpdate.packagesEnabled')" :description="t('settingsPage.autoUpdate.packagesEnabledHint')">
        <template #control><Skeleton class="h-5 w-10 rounded-pill" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.autoUpdate.packagesMode')">
        <template #control><Skeleton class="h-8 w-full sm:w-64" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.autoUpdate.scheduleTime')" :description="t('settingsPage.autoUpdate.scheduleTimeHint')">
        <template #control><Skeleton class="h-8 w-36" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.autoUpdate.lastRun')" :description="t('settingsPage.autoUpdate.lastRunHint')">
        <div class="grid gap-3 text-sm sm:grid-cols-2">
          <div>
            <span class="block text-muted-foreground">{{ t('settingsPage.autoUpdate.lastAutoRunAt') }}</span>
            <Skeleton class="mt-1 h-4 w-32" />
          </div>
          <div>
            <span class="block text-muted-foreground">{{ t('settingsPage.autoUpdate.lastRunAt') }}</span>
            <Skeleton class="mt-1 h-4 w-32" />
          </div>
          <div>
            <span class="block text-muted-foreground">{{ t('settingsPage.autoUpdate.lastRunStatus') }}</span>
            <Skeleton class="mt-1 h-5 w-16 rounded-pill" />
          </div>
          <div>
            <span class="block text-muted-foreground">{{ t('settingsPage.autoUpdate.lastRunTask') }}</span>
            <Skeleton class="mt-1 h-4 w-40" />
          </div>
          <div class="sm:col-span-2">
            <span class="block text-muted-foreground">{{ t('settingsPage.autoUpdate.lastRunSummary') }}</span>
            <Skeleton class="mt-1 h-4 w-64 max-w-full" />
          </div>
        </div>
      </SettingsCard>
    </SettingsSection>

    <SettingsSection v-else-if="section === 'system-certificates'" :description="t('settingsPage.systemCertificatesHint')">
      <div class="p-3">
        <Table
          :framed="false"
          row-height="auto"
          :columns="systemCertificateColumns"
          :rows="[]"
          :loading="true"
          :loading-label="t('common.loading')"
        />
      </div>
    </SettingsSection>

    <SettingsSection v-else-if="section === 'system'">
      <SettingsCard>
        <div class="grid gap-3 text-sm">
          <div class="grid grid-cols-2 gap-3 max-md:grid-cols-1">
            <div><span class="text-muted-foreground">{{ t('settingsPage.panelVersion') }}</span><Skeleton class="mt-1 h-4 w-24" /></div>
            <div><span class="text-muted-foreground">{{ t('settingsPage.releaseChannel') }}</span><Skeleton class="mt-1 h-4 w-20" /></div>
            <div><span class="text-muted-foreground">{{ t('settingsPage.latestVersion') }}</span><Skeleton class="mt-1 h-4 w-24" /></div>
            <div><span class="text-muted-foreground">{{ t('settingsPage.updateStatus') }}</span><Skeleton class="mt-1 h-5 w-20 rounded-pill" /></div>
          </div>
        </div>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.loginTitle')">
        <template #control><Skeleton class="h-8 w-full sm:w-72" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.loginSubtitle')">
        <template #control><Skeleton class="h-8 w-full sm:w-72" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.serverVariables')" stacked>
        <template #control><Skeleton class="h-44 w-full" /></template>
      </SettingsCard>
    </SettingsSection>

    <SettingsSection v-else-if="section === 'image-registry'" :description="t('settingsPage.imageRegistry.hint')">
      <SettingsCard :title="t('settingsPage.imageRegistry.endpoint')" :description="t('settingsPage.imageRegistry.endpointHint')">
        <template #control><Skeleton class="h-8 w-full sm:w-72" /></template>
      </SettingsCard>
      <SettingsCard :title="t('settingsPage.imageRegistry.scheme')">
        <template #control><Skeleton class="h-8 w-full sm:w-64" /></template>
      </SettingsCard>
    </SettingsSection>

    <div v-else class="grid gap-4">
      <SettingsSection :title="t('settingsPage.backupExport')">
        <SettingsCard :title="t('settingsPage.encryptExport')">
          <template #control><Skeleton class="h-5 w-10 rounded-pill" /></template>
        </SettingsCard>
        <SettingsCard :title="t('settingsPage.backupPassword')">
          <template #control><Skeleton class="h-8 w-full sm:w-64" /></template>
        </SettingsCard>
        <template #footer>
          <Button class="max-sm:w-full" variant="primary" disabled><DatabaseBackup />{{ t('settingsPage.startExport') }}</Button>
        </template>
      </SettingsSection>
      <SettingsSection :title="t('settingsPage.restore')">
        <SettingsCard :title="t('settingsPage.backupPassword')">
          <template #control><Skeleton class="h-8 w-full sm:w-64" /></template>
        </SettingsCard>
        <SettingsCard stacked>
          <template #control><Skeleton class="h-8 w-44" /></template>
        </SettingsCard>
        <template #footer>
          <Button class="max-sm:w-full" variant="danger" disabled><UploadCloud />{{ t('settingsPage.confirmRestore') }}</Button>
        </template>
      </SettingsSection>
    </div>
  </div>
</template>
