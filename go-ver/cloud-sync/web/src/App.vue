<script setup>
import { RouterView } from 'vue-router'
import SidebarNav from './components/admin/SidebarNav.vue'
import TopBar from './components/admin/TopBar.vue'
import ToastStack from './components/admin/ToastStack.vue'
import { useSyncAdmin } from './composables/useSyncAdmin'

const admin = useSyncAdmin()
</script>

<template>
  <div class="h-screen bg-muted text-foreground">
    <div class="grid h-full grid-cols-1 md:grid-cols-[240px_1fr]">
      <SidebarNav />

      <div class="flex min-h-0 flex-col">
        <TopBar
          :user-id="admin.userId.value"
          :since-version="admin.sinceVersion.value"
          :cursor="admin.cursor.value"
          :sync-busy="admin.busy.syncLoop"
          :on-sync="admin.doSyncLoop"
        />

        <main class="min-h-0 flex-1 overflow-auto p-6">
          <RouterView />
        </main>
      </div>
    </div>

    <ToastStack :toasts="admin.toasts.value" @dismiss="admin.dismissToast" />
  </div>
</template>
