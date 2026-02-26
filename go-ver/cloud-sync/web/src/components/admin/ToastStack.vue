<script setup>
import UiButton from '../ui/Button.vue'

defineProps({
  toasts: { type: Array, default: () => [] },
})

const emit = defineEmits(['dismiss'])
</script>

<template>
  <div class="pointer-events-none fixed right-4 top-4 z-50 flex w-80 flex-col gap-2">
    <div
      v-for="toast in toasts"
      :key="toast.id"
      class="pointer-events-auto rounded-lg border bg-white p-3 shadow-card"
      :class="toast.type === 'error' ? 'border-destructive/40' : 'border-border'"
    >
      <div class="flex items-start justify-between gap-2">
        <div>
          <p class="text-sm font-medium">{{ toast.title }}</p>
          <p class="mt-1 text-xs text-muted-foreground">{{ toast.description }}</p>
        </div>
        <UiButton variant="ghost" size="sm" @click="emit('dismiss', toast.id)">Close</UiButton>
      </div>
    </div>
  </div>
</template>
