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
      <h2 class="text-base font-semibold">Sync Jobs</h2>
      <p class="mt-1 text-sm text-muted-foreground">Run handshake, incremental pull, and write operations.</p>

      <div class="mt-4 grid gap-4 lg:grid-cols-2">
        <div class="space-y-2 rounded-lg border border-border p-4">
          <h3 class="text-sm font-semibold">Pull Controls</h3>
          <div class="grid gap-2 md:grid-cols-2">
            <UiInput v-model="admin.pullListLimit.value" placeholder="list limit" type="number" />
            <UiInput v-model="admin.pullDeltaLimit.value" placeholder="delta limit" type="number" />
          </div>
          <div class="flex flex-wrap gap-2">
            <UiButton variant="secondary" :disabled="admin.busy.handshake" @click="admin.doHandshake">
              {{ admin.busy.handshake ? 'Running…' : 'Handshake' }}
            </UiButton>
            <UiButton :disabled="admin.busy.syncLoop" @click="admin.doSyncLoop">
              {{ admin.busy.syncLoop ? 'Running…' : 'Sync Loop' }}
            </UiButton>
            <UiButton variant="outline" :disabled="admin.busy.pullList" @click="admin.doPullList">
              {{ admin.busy.pullList ? 'Running…' : 'List Items' }}
            </UiButton>
            <UiButton variant="outline" :disabled="admin.busy.pullDelta" @click="admin.doPullDelta">
              {{ admin.busy.pullDelta ? 'Running…' : 'Delta Events' }}
            </UiButton>
          </div>
        </div>

        <div class="space-y-2 rounded-lg border border-border p-4">
          <h3 class="text-sm font-semibold">Upsert Item</h3>
          <UiInput v-model="admin.itemPath.value" placeholder="/notes/todo.md" />
          <textarea
            v-model="admin.itemMetadata.value"
            rows="4"
            class="w-full rounded-md border border-input bg-white px-3 py-2 text-sm"
            placeholder='{"title":"Todo"}'
          />
          <UiInput v-model="admin.upsertBaseVersion.value" type="number" placeholder="base_version (optional)" />
          <UiButton :disabled="admin.busy.upsert" @click="admin.doUpsert">
            {{ admin.busy.upsert ? 'Running…' : 'Upsert' }}
          </UiButton>
        </div>
      </div>
    </UiCard>

    <UiCard>
      <h2 class="text-base font-semibold">Delete / Restore</h2>
      <div class="mt-4 grid gap-4 lg:grid-cols-2">
        <div class="space-y-2 rounded-lg border border-border p-4">
          <h3 class="text-sm font-semibold">Delete</h3>
          <UiInput v-model="admin.deleteId.value" placeholder="item id" />
          <UiInput v-model="admin.deleteBaseVersion.value" placeholder="base_version (optional)" type="number" />
          <UiButton variant="destructive" :disabled="admin.busy.delete" @click="admin.doDelete">
            {{ admin.busy.delete ? 'Running…' : 'Delete' }}
          </UiButton>
        </div>

        <div class="space-y-2 rounded-lg border border-border p-4">
          <h3 class="text-sm font-semibold">Restore</h3>
          <UiInput v-model="admin.restoreId.value" placeholder="item id" />
          <UiInput v-model="admin.restoreBaseVersion.value" placeholder="base_version (optional)" type="number" />
          <UiButton variant="secondary" :disabled="admin.busy.restore" @click="admin.doRestore">
            {{ admin.busy.restore ? 'Running…' : 'Restore' }}
          </UiButton>
        </div>
      </div>
    </UiCard>

    <UiCard>
      <div class="flex items-center justify-between gap-3">
        <div>
          <h2 class="text-base font-semibold">Conflict Resolve</h2>
          <p class="text-sm text-muted-foreground">Handle 409 with server context and merged metadata.</p>
        </div>
        <span
          v-if="admin.pendingConflict.value"
          class="rounded-full bg-red-50 px-2 py-1 text-xs text-red-700"
        >
          server_version={{ admin.pendingConflict.value.serverVersion }} ·
          server_hash={{ admin.pendingConflict.value.serverHash }}
        </span>
      </div>

      <div class="mt-4 grid gap-2 md:grid-cols-2">
        <UiInput v-model="admin.resolveId.value" placeholder="id (optional)" />
        <UiInput v-model="admin.resolvePath.value" placeholder="path (if id empty)" />
      </div>
      <textarea
        v-model="admin.resolveMetadata.value"
        rows="3"
        class="mt-2 w-full rounded-md border border-input bg-white px-3 py-2 text-sm"
        placeholder='{"merged":true}'
      />
      <UiInput v-model="admin.resolveBaseVersion.value" class="mt-2" type="number" placeholder="base_version" />

      <div class="mt-3">
        <UiButton :disabled="admin.busy.resolve" @click="admin.doResolve">
          {{ admin.busy.resolve ? 'Running…' : 'Resolve Conflict' }}
        </UiButton>
      </div>
    </UiCard>
  </div>
</template>
