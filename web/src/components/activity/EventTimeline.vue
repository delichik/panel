<script setup lang="ts">
import { useErrorToast, useSuccessToast } from '@/components/ui/toast';
import { computed } from 'vue';
import Badge from '@/components/ui/Badge.vue';
import Button from '@/components/ui/Button.vue';
import type { ActivityEvent } from '@/types/activity';
import { eventDisplayMessage, eventMessage, eventStream, eventTone } from '@/views/activity/model';
import { translateEventSummary, useI18n } from '@/i18n';
import { formatDateTime } from '@/utils/datetime';
const props = defineProps<{ events: ActivityEvent[]; selectedEventId?: string }>();
const emit = defineEmits<{ context: [event: ActivityEvent]; evidence: [evidenceId: string] }>();
const { t } = useI18n();
const notifyError = useErrorToast();
const notifySuccess = useSuccessToast();
async function copy(event: ActivityEvent) { try { await navigator.clipboard.writeText(event.kind === 'output' ? eventMessage(event) : eventDisplayMessage(event, t)); notifySuccess(t('activity.copied')); } catch (err) { notifyError(err instanceof Error ? err.message : t('common.operationFailed'), err); } }
const groups = computed(() => {
  const result: Array<{ key: string; events: ActivityEvent[]; output: boolean }> = [];
  for (const event of props.events) {
    const previous = result.at(-1);
    if (event.kind === 'output' && previous?.output && previous.events[0]?.stepId === event.stepId && previous.events[0]?.executionId === event.executionId) previous.events.push(event);
    else result.push({ key: event.eventId, events: [event], output: event.kind === 'output' });
  }
  return result;
});
function message(event: ActivityEvent) {
  if (event.messageCode) {
    const translated = t(event.messageCode, event.messageArgs);
    if (translated !== event.messageCode) return translated;
  }
  return event.kind === 'output' ? eventMessage(event) : translateEventSummary(t, eventDisplayMessage(event, t));
}
</script>

<template>
  <ol class="m-0 grid min-w-0 list-none gap-3 p-0" :aria-label="t('activity.timeline')">
    <li v-for="group in groups" :key="group.key" class="min-w-0 rounded-xl border border-border bg-card" :class="group.events.some(event => event.eventId === selectedEventId) ? 'ring-2 ring-ring/40' : ''">
      <details v-if="group.output" :open="group.events.some(event => event.level === 'error' || event.eventId === selectedEventId)" class="min-w-0 p-3">
        <summary class="cursor-pointer text-sm font-medium">{{ t('activity.outputCount', { count: group.events.length }) }}</summary>
        <div class="mt-3 grid min-w-0 gap-2">
          <div v-for="event in group.events" :key="event.eventId" class="min-w-0">
            <div class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground"><time :datetime="event.occurredAt">{{ formatDateTime(event.occurredAt) }}</time><Badge :tone="eventTone(event.level)">{{ eventStream(event) || t(`activity.level.${event.level}`) }}</Badge><Button size="sm" variant="ghost" @click="emit('context', event)">{{ t('activity.context') }}</Button><Button size="sm" variant="ghost" @click="copy(event)">{{ t('activity.copy') }}</Button></div>
            <pre class="m-0 max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-muted p-2 font-mono text-xs [overflow-wrap:anywhere]">{{ message(event) }}</pre><details class="mt-2 text-xs text-muted-foreground"><summary class="cursor-pointer">{{ t('activity.technical') }}</summary><pre class="max-h-64 overflow-auto whitespace-pre-wrap break-words [overflow-wrap:anywhere]">{{ JSON.stringify(event, null, 2) }}</pre></details>
          </div>
        </div>
      </details>
      <div v-else v-for="event in group.events" :key="event.eventId" class="min-w-0 p-3">
        <div class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground"><time :datetime="event.occurredAt">{{ formatDateTime(event.occurredAt) }}</time><Badge :tone="eventTone(event.level)">{{ t(`activity.level.${event.level}`) }}</Badge><span>{{ event.actor?.name || event.actor?.id || event.sourceId }}</span></div>
        <p class="my-2 whitespace-pre-wrap break-words text-sm [overflow-wrap:anywhere]">{{ message(event) }}</p>
        <div class="flex flex-wrap gap-1"><Badge v-for="resource in event.resources" :key="`${resource.resourceType}:${resource.resourceId}:${resource.role}`">{{ resource.nameSnapshot || resource.resourceId }}<span v-if="resource.revisionId"> · {{ resource.revisionId }}</span></Badge></div>
        <details class="mt-2 min-w-0 text-xs text-muted-foreground" :open="event.kind === 'decision' || event.level === 'error'"><summary class="cursor-pointer">{{ t('activity.evidence') }}</summary><pre v-if="event.data && Object.keys(event.data).length" class="mt-2 max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-muted p-2 [overflow-wrap:anywhere]">{{ JSON.stringify(event.data, null, 2) }}</pre><Button v-if="typeof event.data?.evidenceId === 'string'" size="sm" variant="secondary" @click="emit('evidence', event.data.evidenceId)">{{ t('common.download') }}</Button></details>
        <div class="mt-2 flex flex-wrap items-start gap-2"><Button size="sm" variant="ghost" @click="emit('context', event)">{{ t('activity.context') }}</Button><Button size="sm" variant="ghost" @click="copy(event)">{{ t('activity.copy') }}</Button><details class="min-w-0 flex-1 p-2 text-xs text-muted-foreground"><summary class="cursor-pointer">{{ t('activity.technical') }}</summary><pre class="mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-words [overflow-wrap:anywhere]">{{ JSON.stringify(event, null, 2) }}</pre></details></div>
      </div>
    </li>
  </ol>
</template>
