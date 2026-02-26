<script setup>
import { RouterLink, useRoute } from 'vue-router'
import { computed } from 'vue'

const route = useRoute()

const navItems = [
  { to: '/', label: 'Dashboard' },
  { to: '/environments', label: 'Environments' },
  { to: '/sync-jobs', label: 'Sync Jobs' },
  { to: '/activity', label: 'Activity' },
]

const pageTitle = computed(() => route.meta?.title || 'Admin Center')
const pageDescription = computed(() => route.meta?.description || 'Cloud sync operations')
</script>

<template>
  <aside class="flex h-full flex-col border-r border-border bg-white">
    <div class="border-b border-border px-5 py-4">
      <p class="text-xs font-medium uppercase tracking-wide text-muted-foreground">Cloud Sync</p>
      <h1 class="mt-1 text-base font-semibold">Admin Center</h1>
    </div>

    <nav class="flex-1 space-y-1 p-3">
      <RouterLink
        v-for="item in navItems"
        :key="item.to"
        :to="item.to"
        class="block rounded-md px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
        active-class="bg-muted text-foreground"
      >
        {{ item.label }}
      </RouterLink>
    </nav>

    <div class="border-t border-border px-5 py-4">
      <p class="text-sm font-medium">{{ pageTitle }}</p>
      <p class="mt-1 text-xs text-muted-foreground">{{ pageDescription }}</p>
    </div>
  </aside>
</template>
