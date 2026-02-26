function readEnv(...keys) {
  if (typeof import.meta === 'undefined' || !import.meta.env) return ''
  for (const key of keys) {
    const v = import.meta.env[key]
    if (typeof v === 'string' && v.trim() !== '') return v.trim()
  }
  return ''
}

function readWindowConfig(...keys) {
  if (typeof window === 'undefined' || !window.__CLOUD_SYNC_CONFIG__) return ''
  const cfg = window.__CLOUD_SYNC_CONFIG__
  for (const key of keys) {
    const v = cfg[key]
    if (typeof v === 'string' && v.trim() !== '') return v.trim()
  }
  return ''
}

function normalizeBaseUrl(url) {
  const raw = String(url || '').trim()
  if (!raw) return '/api/cloud-sync/v1'
  return raw.endsWith('/') ? raw.slice(0, -1) : raw
}

function normalizeToken(token) {
  const raw = String(token || '').trim()
  if (!raw) return ''
  if (/^bearer\s+/i.test(raw)) return raw
  return `Bearer ${raw}`
}

const state = {
  baseUrl: normalizeBaseUrl(
    readEnv('VITE_CLOUD_SYNC_BASE_URL', 'CLOUD_SYNC_BASE_URL') ||
      readWindowConfig('baseUrl', 'CLOUD_SYNC_BASE_URL') ||
      '/api/cloud-sync/v1',
  ),
  token:
    readEnv('VITE_CLOUD_SYNC_TOKEN', 'CLOUD_SYNC_TOKEN') ||
    readWindowConfig('token', 'CLOUD_SYNC_TOKEN') ||
    '',
  userId:
    readEnv('VITE_CLOUD_SYNC_USER_ID', 'CLOUD_SYNC_USER_ID') ||
    readWindowConfig('userId', 'CLOUD_SYNC_USER_ID') ||
    'demo-user',
}

export function getApiConfig() {
  return { ...state }
}

export function setApiConfig(next) {
  if (!next || typeof next !== 'object') return getApiConfig()
  if (next.baseUrl != null) state.baseUrl = normalizeBaseUrl(next.baseUrl)
  if (next.token != null) state.token = String(next.token)
  if (next.userId != null) state.userId = String(next.userId)
  return getApiConfig()
}

async function request(path, options = {}) {
  const token = normalizeToken(options.token ?? state.token)
  const userId = String(options.userId ?? state.userId ?? '').trim()

  const headers = {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: token } : {}),
    ...(userId ? { 'X-User-ID': userId } : {}),
    ...(options.headers ?? {}),
  }

  const res = await fetch(`${state.baseUrl}${path}`, {
    ...options,
    headers,
  })

  const text = await res.text()
  let body
  try {
    body = text ? JSON.parse(text) : {}
  } catch {
    body = { raw: text }
  }

  if (!res.ok) {
    const err = new Error(body.error ?? `HTTP ${res.status}`)
    err.status = res.status
    err.payload = body
    throw err
  }
  return body
}

export function handshake(payload, userId) {
  return request('/handshake', { method: 'POST', body: JSON.stringify(payload), userId })
}

export function upsertItem(payload, userId) {
  return request('/items', { method: 'POST', body: JSON.stringify(payload), userId })
}

export function listItems(params, userId) {
  const query = new URLSearchParams()
  Object.entries(params || {}).forEach(([k, v]) => {
    if (v !== undefined && v !== null && v !== '') query.set(k, String(v))
  })
  return request(`/items?${query.toString()}`, { userId })
}

export function getItem(id, userId) {
  return request(`/items/${id}`, { userId })
}

export function deleteItem(id, baseVersion, userId) {
  return request(`/items/${id}/delete`, {
    method: 'POST',
    body: JSON.stringify(baseVersion != null ? { base_version: baseVersion } : {}),
    userId,
  })
}

export function restoreItem(id, baseVersion, userId) {
  return request(`/items/${id}/restore`, {
    method: 'POST',
    body: JSON.stringify(baseVersion != null ? { base_version: baseVersion } : {}),
    userId,
  })
}

export function delta(payload, userId) {
  return request('/delta', { method: 'POST', body: JSON.stringify(payload), userId })
}

export function resolveConflict(payload, userId) {
  return request('/conflict/resolve', { method: 'POST', body: JSON.stringify(payload), userId })
}
