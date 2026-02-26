<script setup>
import { computed } from 'vue'
import UiCard from '../components/ui/Card.vue'
import MetricCard from '../components/admin/MetricCard.vue'
import UiButton from '../components/ui/Button.vue'
import { useSyncAdmin } from '../composables/useSyncAdmin'

const admin = useSyncAdmin()

const eventCount = computed(() => admin.events.value.length)
const itemCount = computed(() => admin.items.value.length)
</script>

<template>
  <div class="space-y-4">
    <section class="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
      <MetricCard label="Latest Version" :value="admin.latestVersion.value" hint="Derived from sync state + items" />
      <MetricCard label="Items" :value="itemCount" hint="Current list snapshot" />
      <MetricCard label="Delta Events" :value="eventCount" hint="Latest delta response" />
      <MetricCard label="Conflicts" :value="admin.conflictCount.value" hint="Pending + event conflicts" />
    </section>

    <UiCard>
      <div class="flex items-center justify-between gap-3">
        <div>
          <h2 class="text-base font-semibold">Sync Health</h2>
          <p class="text-sm text-muted-foreground">
            Last updated: {{ admin.lastUpdatedAt.value || 'No operations yet' }}
          </p>
        </div>
        <UiButton :disabled="admin.busy.syncLoop" @click="admin.doSyncLoop">
          {{ admin.busy.syncLoop ? 'Running…' : 'Run Sync Loop' }}
        </UiButton>
      </div>
    </UiCard>

    <UiCard>
      <h2 class="text-base font-semibold">Recent Jobs</h2>
      <div class="mt-3 overflow-x-auto">
        <table class="min-w-full text-sm">
          <thead>
            <tr class="border-b border-border text-left text-muted-foreground">
              <th class="pb-2 pr-3 font-medium">Operation</th>
              <th class="pb-2 pr-3 font-medium">Status</th>
              <th class="pb-2 pr-3 font-medium">Time</th>
              <th class="pb-2 font-medium">Detail</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in admin.operationLog.value.slice(0, 8)" :key="row.id" class="border-b border-border/60">
              <td class="py-2 pr-3">{{ row.name }}</td>
              <td class="py-2 pr-3">
                <span
                  class="rounded-full px-2 py-0.5 text-xs"
                  :class="row.status === 'success' ? 'bg-emerald-50 text-emerald-700' : 'bg-red-50 text-red-700'"
                >
                  {{ row.status }}
                </span>
              </td>
              <td class="py-2 pr-3 text-muted-foreground">{{ row.at }}</td>
              <td class="py-2 text-muted-foreground">{{ row.detail || '-' }}</td>
            </tr>
            <tr v-if="!admin.operationLog.value.length">
              <td colspan="4" class="py-4 text-center text-sm text-muted-foreground">No operations yet.</td>
            </tr>
          </tbody>
        </table>
      </div>
    </UiCard>
  </div>
</template>
