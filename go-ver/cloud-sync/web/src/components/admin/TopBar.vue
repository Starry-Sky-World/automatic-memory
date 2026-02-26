<script setup>
import { computed } from 'vue'
import UiButton from '../ui/Button.vue'

const props = defineProps({
  userId: { type: String, default: '' },
  sinceVersion: { type: Number, default: 0 },
  cursor: { type: Number, default: 0 },
  onSync: { type: Function, required: true },
  syncBusy: { type: Boolean, default: false },
})

const stateLabel = computed(() => `since v${props.sinceVersion} · cursor ${props.cursor}`)
</script>

<template>
  <header class="flex flex-wrap items-center justify-between gap-3 border-b border-border bg-white px-6 py-3">
    <div>
      <p class="text-sm font-medium">{{ userId || 'unknown user' }}</p>
      <p class="text-xs text-muted-foreground">{{ stateLabel }}</p>
    </div>

    <div class="flex items-center gap-2">
      <UiButton variant="secondary" size="sm" :disabled="syncBusy" @click="onSync">
        {{ syncBusy ? 'Running…' : 'Run Sync Loop' }}
      </UiButton>
    </div>
  </header>
</template>
