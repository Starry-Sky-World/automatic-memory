import { createRouter, createWebHistory } from 'vue-router'
import DashboardView from '../views/DashboardView.vue'
import EnvironmentsView from '../views/EnvironmentsView.vue'
import SyncJobsView from '../views/SyncJobsView.vue'
import ActivityView from '../views/ActivityView.vue'

const routes = [
  {
    path: '/',
    name: 'dashboard',
    component: DashboardView,
    meta: { title: 'Dashboard', description: 'Sync overview and key metrics' },
  },
  {
    path: '/environments',
    name: 'environments',
    component: EnvironmentsView,
    meta: { title: 'Environments', description: 'Connection and auth settings' },
  },
  {
    path: '/sync-jobs',
    name: 'sync-jobs',
    component: SyncJobsView,
    meta: { title: 'Sync Jobs', description: 'Run sync operations and resolve conflicts' },
  },
  {
    path: '/activity',
    name: 'activity',
    component: ActivityView,
    meta: { title: 'Activity', description: 'Items and delta timeline' },
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

export default router
