<script setup>
import UiCard from '../components/ui/Card.vue'
import UiInput from '../components/ui/Input.vue'
import UiButton from '../components/ui/Button.vue'
import { useSyncAdmin } from '../composables/useSyncAdmin'

const admin = useSyncAdmin()
</script>

<template>
  <div class="space-y-4">
    <UiCard>
      <h2 class="text-base font-semibold">Environment Configuration</h2>
      <p class="mt-1 text-sm text-muted-foreground">
        Configure API base URL and request headers. Reuses existing api.js config behavior.
      </p>

      <div class="mt-4 space-y-3">
        <div>
          <p class="mb-1 text-xs text-muted-foreground">Base URL</p>
          <UiInput v-model="admin.baseUrl.value" placeholder="/api/cloud-sync/v1" />
        </div>

        <div>
          <p class="mb-1 text-xs text-muted-foreground">Token (Bearer optional)</p>
          <UiInput v-model="admin.token.value" placeholder="Bearer ..." />
        </div>

        <div class="grid gap-3 md:grid-cols-2">
          <div>
            <p class="mb-1 text-xs text-muted-foreground">X-User-ID</p>
            <UiInput v-model="admin.userId.value" placeholder="demo-user" />
          </div>
          <div>
            <p class="mb-1 text-xs text-muted-foreground">Device ID</p>
            <UiInput v-model="admin.deviceId.value" placeholder="web-client" />
          </div>
        </div>
      </div>

      <div class="mt-4 flex justify-end">
        <UiButton @click="admin.applyConfig">Apply Environment</UiButton>
      </div>
    </UiCard>

    <UiCard>
      <h3 class="text-sm font-semibold">State Snapshot</h3>
      <div class="mt-3 grid gap-3 md:grid-cols-3">
        <div class="rounded-md border border-border bg-muted p-3">
          <p class="text-xs text-muted-foreground">since_version</p>
          <p class="mt-1 text-lg font-semibold">{{ admin.sinceVersion.value }}</p>
        </div>
        <div class="rounded-md border border-border bg-muted p-3">
          <p class="text-xs text-muted-foreground">cursor</p>
          <p class="mt-1 text-lg font-semibold">{{ admin.cursor.value }}</p>
        </div>
        <div class="rounded-md border border-border bg-muted p-3">
          <p class="text-xs text-muted-foreground">latest_version</p>
          <p class="mt-1 text-lg font-semibold">{{ admin.latestVersion.value }}</p>
        </div>
      </div>
    </UiCard>
  </div>
</template>
