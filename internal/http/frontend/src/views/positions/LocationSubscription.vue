<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->

<template>
  <Card>
    <CardHeader>
      <CardTitle>LRRP subscription</CardTitle>
      <CardDescription>Periodic location requests and replies from this radio.</CardDescription>
    </CardHeader>
    <CardContent class="flex flex-col gap-3">
      <template v-if="status">
        <p class="text-sm font-medium" data-test="subscription-state">{{ labels[status.state] }}</p>
        <p v-if="status.detail" class="text-sm text-muted-foreground">{{ status.detail }}</p>
        <dl v-if="status.request_id" class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
          <dt>Request ID</dt><dd>{{ status.request_id }}</dd>
          <dt>Service ID</dt><dd>{{ status.service_id }}</dd>
          <template v-if="status.interval_seconds">
            <dt>Requested interval</dt><dd>{{ status.interval_seconds / 60 }} min</dd>
          </template>
          <template v-if="status.sent_at_ms">
            <dt>Last request</dt><dd>{{ new Date(status.sent_at_ms).toLocaleString() }}</dd>
          </template>
          <template v-if="status.acknowledged_at_ms">
            <dt>Accepted</dt><dd>{{ new Date(status.acknowledged_at_ms).toLocaleString() }}</dd>
          </template>
          <template v-if="status.reported_at_ms">
            <dt>Last periodic report</dt><dd>{{ new Date(status.reported_at_ms).toLocaleString() }}</dd>
          </template>
        </dl>
        <p v-if="!status.available" class="text-sm text-muted-foreground">The built-in Location server is disabled.</p>
        <p v-else-if="status.online === false" class="text-sm text-muted-foreground">
          The Location server is offline. Requests can wait for it to reconnect.
        </p>
        <div v-if="status.available" class="flex flex-wrap gap-2">
          <ShadButton type="button" variant="outline" size="sm" :disabled="busy" @click="request('subscribe')">
            {{ status.request_id && status.state !== 'stopped' ? 'Resend subscription' : 'Send subscription' }}
          </ShadButton>
          <ShadButton type="button" variant="outline" size="sm" :disabled="busy" @click="request('once')">
            Request position once
          </ShadButton>
          <ShadButton
            v-if="status.request_id && status.state !== 'stopped'"
            type="button" variant="outline" size="sm" :disabled="busy" @click="request('unsubscribe')"
          >
            Unsubscribe
          </ShadButton>
        </div>
        <p v-if="status.command" role="status" class="text-sm text-muted-foreground">
          {{ actions[status.command.action] }} · {{ labels[status.command.state] }}
          <template v-if="status.command.detail"> · {{ status.command.detail }}</template>
        </p>
      </template>
      <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>
    </CardContent>
  </Card>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';

import { Button as ShadButton } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { getLocationSubscription, queueLocationCommand, type LocationAction, type SubscriptionStatus } from '@/services/positions';
import { errorText } from '@/services/settings';

const props = defineProps<{ radio: number }>();
const status = ref<SubscriptionStatus>();
const error = ref('');
const sending = ref(false);
const busy = computed(() =>
  sending.value || Boolean(status.value?.retry_at_ms) || ['queued', 'pending'].includes(status.value?.command?.state ?? ''),
);

const labels: Record<string, string> = {
  off: 'Tracking is off',
  configured: 'Configured',
  queued: 'Queued',
  pending: 'Waiting for LRRP reply',
  accepted: 'Accepted by the radio',
  reporting: 'Receiving periodic reports',
  reported: 'Position received',
  stale: 'Stale',
  failed: 'Failed',
  stopping: 'Waiting for LRRP stop reply',
  stopped: 'Unsubscribed',
};
const actions: Record<LocationAction, string> = { subscribe: 'Subscription request', once: 'One-time request', unsubscribe: 'Unsubscribe request' };
let revision = 0;

async function refresh() {
  if (sending.value) return;
  const current = ++revision;
  try {
    const next = await getLocationSubscription(props.radio);
    if (current === revision) {
      if (!status.value) error.value = '';
      status.value = next;
    }
  } catch {
    if (current === revision && !status.value) error.value = 'Could not load subscription status.';
  }
}

async function request(action: LocationAction) {
  revision++;
  sending.value = true;
  error.value = '';
  try {
    const command = await queueLocationCommand(props.radio, action);
    if (status.value) status.value = { ...status.value, command };
  } catch (err) {
    error.value = errorText(err, 'Could not queue the request.');
  } finally {
    sending.value = false;
    void refresh();
  }
}

let timer: ReturnType<typeof setInterval>;
onMounted(() => {
  void refresh();
  timer = setInterval(refresh, 5000);
});
onUnmounted(() => clearInterval(timer));
</script>
