<script setup>
defineProps({
  label: { type: String, required: true },
  value: { type: [String, Number], required: true },
  hint: { type: String, default: '' },
})
</script>

<template>
  <div class="rounded-lg border border-border bg-white p-4">
    <p class="text-xs text-muted-foreground">{{ label }}</p>
    <p class="mt-1 text-2xl font-semibold tracking-tight">{{ value }}</p>
    <p v-if="hint" class="mt-1 text-xs text-muted-foreground">{{ hint }}</p>
  </div>
</template>
