import { computed, reactive, ref, watch } from 'vue'
import {
  getApiConfig,
  setApiConfig,
  upsertItem,
  listItems,
  delta,
  deleteItem,
  restoreItem,
  resolveConflict,
} from '../lib/api'
import { loadSyncState, saveSyncState, runHandshake, pullIncremental } from '../lib/sync'

function safeJson(value) {
  try {
    return JSON.parse(value)
  } catch {
    return {}
  }
}

function formatNow() {
  return new Date().toISOString()
}

function formatError(err) {
  return {
    ok: false,
    status: err?.status ?? 0,
    message: err?.message ?? 'Request failed',
    payload: err?.payload ?? null,
  }
}

function getErrorCopy(status) {
  if (status === 401) {
    return {
      title: 'Unauthorized',
      description: 'Token is invalid or missing. Update auth settings in Environments.',
    }
  }
  if (status === 404) {
    return {
      title: 'Not found',
      description: 'The target item or endpoint was not found. Check IDs and base URL.',
    }
  }
  if (status === 409) {
    return {
      title: 'Conflict detected',
      description: 'Server version is newer. Review conflict context and resolve with merged metadata.',
    }
  }
  return {
    title: 'Request failed',
    description: 'Operation failed. Check response payload in Advanced Debug.',
  }
}

let instance

export function useSyncAdmin() {
  if (instance) return instance

  const initialConfig = getApiConfig()

  const baseUrl = ref(initialConfig.baseUrl)
  const token = ref(initialConfig.token)
  const userId = ref(initialConfig.userId)
  const deviceId = ref('web-client')

  const syncState = ref(loadSyncState(userId.value))
  const sinceVersion = ref(syncState.value.sinceVersion)
  const cursor = ref(syncState.value.cursor)

  const pullListLimit = ref(50)
  const pullDeltaLimit = ref(100)

  const itemPath = ref('/notes/todo.md')
  const itemMetadata = ref('{"title":"Todo","tags":["demo"]}')
  const upsertBaseVersion = ref('')

  const deleteId = ref('')
  const deleteBaseVersion = ref('')
  const restoreId = ref('')
  const restoreBaseVersion = ref('')

  const resolveId = ref('')
  const resolvePath = ref('')
  const resolveMetadata = ref('{"merged":true}')
  const resolveBaseVersion = ref(0)

  const items = ref([])
  const events = ref([])
  const output = ref('')
  const pendingConflict = ref(null)
  const lastUpdatedAt = ref('')

  const toasts = ref([])
  const operationLog = ref([])

  const busy = reactive({
    handshake: false,
    syncLoop: false,
    upsert: false,
    pullList: false,
    pullDelta: false,
    delete: false,
    restore: false,
    resolve: false,
  })

  function pushToast(type, title, description) {
    const id = `${Date.now()}-${Math.random().toString(16).slice(2)}`
    toasts.value = [{ id, type, title, description }, ...toasts.value].slice(0, 4)
  }

  function dismissToast(id) {
    toasts.value = toasts.value.filter((t) => t.id !== id)
  }

  function pushLog(name, status, detail = '') {
    operationLog.value = [
      {
        id: `${Date.now()}-${Math.random().toString(16).slice(2)}`,
        name,
        status,
        detail,
        at: formatNow(),
      },
      ...operationLog.value,
    ].slice(0, 30)
  }

  function applyConfig() {
    setApiConfig({
      baseUrl: baseUrl.value,
      token: token.value,
      userId: userId.value,
    })
    pushToast('success', 'Environment updated', 'Base URL and auth headers were applied.')
  }

  function show(v) {
    output.value = JSON.stringify(v, null, 2)
    lastUpdatedAt.value = formatNow()
  }

  function saveState(next) {
    syncState.value = saveSyncState(userId.value, next)
    sinceVersion.value = syncState.value.sinceVersion
    cursor.value = syncState.value.cursor
  }

  function bumpStateFromVersion(version) {
    const v = Number(version)
    if (!Number.isFinite(v)) return
    saveState({
      sinceVersion: Math.max(sinceVersion.value, v),
      cursor: Math.max(cursor.value, v),
    })
  }

  function recordConflict(err, fallbackPath) {
    const serverVersion = Number(err?.payload?.server_version ?? 0)
    const serverHash = String(err?.payload?.server_hash ?? '')
    pendingConflict.value = { serverVersion, serverHash }
    resolveId.value = ''
    resolvePath.value = fallbackPath ?? ''
    resolveBaseVersion.value = serverVersion
    resolveMetadata.value = itemMetadata.value
  }

  async function runAction(actionName, busyKey, action) {
    busy[busyKey] = true
    try {
      setApiConfig({
        baseUrl: baseUrl.value,
        token: token.value,
        userId: userId.value,
      })
      const data = await action()
      show({ ok: true, data })
      pushToast('success', `${actionName} succeeded`, 'Operation completed successfully.')
      pushLog(actionName, 'success')
      return data
    } catch (err) {
      const normalized = formatError(err)
      show(normalized)
      const copy = getErrorCopy(normalized.status)
      pushToast('error', copy.title, copy.description)
      pushLog(actionName, 'error', `${normalized.status} ${normalized.message}`)
      throw err
    } finally {
      busy[busyKey] = false
    }
  }

  async function doHandshake() {
    return runAction('Handshake', 'handshake', async () => {
      const out = await runHandshake({
        userId: userId.value,
        deviceId: deviceId.value,
        cursor: cursor.value,
      })
      saveState(out.state)
      return out
    })
  }

  async function doSyncLoop() {
    return runAction('Sync Loop', 'syncLoop', async () => {
      const hs = await runHandshake({
        userId: userId.value,
        deviceId: deviceId.value,
        cursor: cursor.value,
      })

      const pulled = await pullIncremental({
        userId: userId.value,
        sinceVersion: hs.state.sinceVersion,
        cursor: hs.state.cursor,
        listLimit: Number(pullListLimit.value || 50),
        deltaLimit: Number(pullDeltaLimit.value || 100),
      })

      items.value = pulled.itemsRes.items ?? []
      events.value = pulled.deltaRes.events ?? []
      saveState(pulled.state)

      return {
        handshake: hs.session,
        items_count: items.value.length,
        events_count: events.value.length,
        state: pulled.state,
      }
    })
  }

  async function doUpsert() {
    try {
      return await runAction('Upsert', 'upsert', async () => {
        const payload = {
          path: itemPath.value,
          metadata: safeJson(itemMetadata.value),
        }
        if (upsertBaseVersion.value !== '') payload.base_version = Number(upsertBaseVersion.value)
        const item = await upsertItem(payload, userId.value)
        pendingConflict.value = null
        bumpStateFromVersion(item.version)
        return item
      })
    } catch (err) {
      if (err?.status === 409) recordConflict(err, itemPath.value)
      throw err
    }
  }

  async function doPullList() {
    return runAction('Pull List', 'pullList', async () => {
      const res = await listItems(
        {
          since_version: Number(sinceVersion.value || 0),
          limit: Number(pullListLimit.value || 50),
          cursor: Number(cursor.value || 0),
        },
        userId.value,
      )
      items.value = res.items ?? []
      saveState({
        sinceVersion: Math.max(sinceVersion.value, Number(res.latest_version || 0)),
        cursor: Math.max(cursor.value, Number(res.next_cursor || 0)),
      })
      return { ...res, state: { sinceVersion: sinceVersion.value, cursor: cursor.value } }
    })
  }

  async function doPullDelta() {
    return runAction('Pull Delta', 'pullDelta', async () => {
      const res = await delta(
        {
          since_version: Number(sinceVersion.value || 0),
          limit: Number(pullDeltaLimit.value || 100),
          cursor: Number(cursor.value || 0),
        },
        userId.value,
      )
      events.value = res.events ?? []
      saveState({
        sinceVersion: sinceVersion.value,
        cursor: Math.max(cursor.value, Number(res.next_cursor || 0)),
      })
      return { ...res, state: { sinceVersion: sinceVersion.value, cursor: cursor.value } }
    })
  }

  async function doDelete() {
    try {
      return await runAction('Delete', 'delete', async () => {
        const base = deleteBaseVersion.value === '' ? null : Number(deleteBaseVersion.value)
        const item = await deleteItem(deleteId.value, base, userId.value)
        bumpStateFromVersion(item.version)
        return item
      })
    } catch (err) {
      if (err?.status === 409) recordConflict(err, '')
      throw err
    }
  }

  async function doRestore() {
    try {
      return await runAction('Restore', 'restore', async () => {
        const base = restoreBaseVersion.value === '' ? null : Number(restoreBaseVersion.value)
        const item = await restoreItem(restoreId.value, base, userId.value)
        bumpStateFromVersion(item.version)
        return item
      })
    } catch (err) {
      if (err?.status === 409) recordConflict(err, '')
      throw err
    }
  }

  async function doResolve() {
    return runAction('Resolve Conflict', 'resolve', async () => {
      const payload = {
        id: resolveId.value || undefined,
        path: resolvePath.value || undefined,
        metadata: safeJson(resolveMetadata.value),
        base_version: Number(resolveBaseVersion.value || 0),
      }
      const item = await resolveConflict(payload, userId.value)
      pendingConflict.value = null
      bumpStateFromVersion(item.version)
      return item
    })
  }

  watch(userId, (nextUser) => {
    syncState.value = loadSyncState(nextUser)
    sinceVersion.value = syncState.value.sinceVersion
    cursor.value = syncState.value.cursor
    setApiConfig({ baseUrl: baseUrl.value, token: token.value, userId: nextUser })
  })

  const latestVersion = computed(() => {
    const itemVersions = items.value.map((it) => Number(it?.version || 0))
    const maxItemVersion = itemVersions.length ? Math.max(...itemVersions) : 0
    return Math.max(Number(sinceVersion.value || 0), maxItemVersion)
  })

  const conflictCount = computed(() => {
    const eventConflicts = events.value.filter((event) => event?.type === 'conflict').length
    return eventConflicts + (pendingConflict.value ? 1 : 0)
  })

  const deletedItemsCount = computed(() => items.value.filter((it) => it?.deleted).length)

  const hasError = computed(() => {
    const firstToast = toasts.value[0]
    return firstToast?.type === 'error'
  })

  instance = {
    baseUrl,
    token,
    userId,
    deviceId,
    sinceVersion,
    cursor,
    pullListLimit,
    pullDeltaLimit,
    itemPath,
    itemMetadata,
    upsertBaseVersion,
    deleteId,
    deleteBaseVersion,
    restoreId,
    restoreBaseVersion,
    resolveId,
    resolvePath,
    resolveMetadata,
    resolveBaseVersion,
    items,
    events,
    output,
    pendingConflict,
    toasts,
    operationLog,
    busy,
    latestVersion,
    conflictCount,
    deletedItemsCount,
    hasError,
    lastUpdatedAt,
    applyConfig,
    dismissToast,
    doHandshake,
    doSyncLoop,
    doUpsert,
    doPullList,
    doPullDelta,
    doDelete,
    doRestore,
    doResolve,
  }

  return instance
}
