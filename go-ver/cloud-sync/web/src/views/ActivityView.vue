<script setup>
import { computed } from 'vue'
import UiCard from '../components/ui/Card.vue'
import { useSyncAdmin } from '../composables/useSyncAdmin'

const admin = useSyncAdmin()

const normalizedEvents = computed(() =>
  admin.events.value.map((event, index) => ({
    id: `${event?.id || 'ev'}-${index}`,
    type: event?.type || 'unknown',
    path: event?.path || '-',
    version: event?.version ?? '-',
    raw: event,
  })),
)
</script>

<template>
  <div class="space-y-4">
    <UiCard>
      <h2 class="text-base font-semibold">Activity Timeline</h2>
      <p class="mt-1 text-sm text-muted-foreground">
        Latest delta events and item snapshot from existing sync API contracts.
      </p>

      <div class="mt-4 space-y-2">
        <div
          v-for="event in normalizedEvents"
          :key="event.id"
          class="rounded-lg border border-border bg-white px-4 py-3"
        >
          <div class="flex items-center justify-between gap-3">
            <p class="text-sm font-medium">{{ event.type }} · {{ event.path }}</p>
            <span class="text-xs text-muted-foreground">v{{ event.version }}</span>
          </div>
        </div>

        <div v-if="!normalizedEvents.length" class="rounded-lg border border-dashed border-border p-6 text-center text-sm text-muted-foreground">
          No events available. Run Delta Events or Sync Loop in Sync Jobs.
        </div>
      </div>
    </UiCard>

    <UiCard>
      <h3 class="text-sm font-semibold">Items Snapshot</h3>
      <div class="mt-3 overflow-x-auto">
        <table class="min-w-full text-sm">
          <thead>
            <tr class="border-b border-border text-left text-muted-foreground">
              <th class="pb-2 pr-3 font-medium">Path</th>
              <th class="pb-2 pr-3 font-medium">ID</th>
              <th class="pb-2 pr-3 font-medium">Version</th>
              <th class="pb-2 font-medium">State</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in admin.items.value" :key="item.id" class="border-b border-border/60">
              <td class="py-2 pr-3">{{ item.path }}</td>
              <td class="py-2 pr-3 text-xs text-muted-foreground">{{ item.id }}</td>
              <td class="py-2 pr-3">{{ item.version }}</td>
              <td class="py-2">
                <span
                  class="rounded-full px-2 py-0.5 text-xs"
                  :class="item.deleted ? 'bg-slate-100 text-slate-700' : 'bg-emerald-50 text-emerald-700'"
                >
                  {{ item.deleted ? 'deleted' : 'active' }}
                </span>
              </td>
            </tr>
            <tr v-if="!admin.items.value.length">
              <td colspan="4" class="py-4 text-center text-sm text-muted-foreground">No items loaded.</td>
            </tr>
          </tbody>
        </table>
      </div>
    </UiCard>

    <UiCard>
      <details>
        <summary class="cursor-pointer text-sm font-semibold">Advanced / Debug</summary>
        <pre class="mt-3 overflow-auto rounded-md bg-[#111827] p-3 text-xs text-gray-100">{{ admin.output.value }}</pre>
      </details>
    </UiCard>
  </div>
</template>
