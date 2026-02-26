import { delta, handshake, listItems } from './api'

const STORAGE_PREFIX = 'cloud-sync.state'

function asInt(v, fallback = 0) {
  const n = Number(v)
  return Number.isFinite(n) && n >= 0 ? Math.floor(n) : fallback
}

function keyForUser(userId) {
  const normalized = String(userId || 'default').trim() || 'default'
  return `${STORAGE_PREFIX}:${normalized}`
}

export function loadSyncState(userId) {
  if (typeof localStorage === 'undefined') {
    return { sinceVersion: 0, cursor: 0 }
  }
  try {
    const raw = localStorage.getItem(keyForUser(userId))
    if (!raw) return { sinceVersion: 0, cursor: 0 }
    const parsed = JSON.parse(raw)
    return {
      sinceVersion: asInt(parsed.sinceVersion, 0),
      cursor: asInt(parsed.cursor, 0),
    }
  } catch {
    return { sinceVersion: 0, cursor: 0 }
  }
}

export function saveSyncState(userId, state) {
  const normalized = {
    sinceVersion: asInt(state?.sinceVersion, 0),
    cursor: asInt(state?.cursor, 0),
  }
  if (typeof localStorage !== 'undefined') {
    localStorage.setItem(keyForUser(userId), JSON.stringify(normalized))
  }
  return normalized
}

export async function runHandshake({ userId, deviceId, cursor }) {
  const session = await handshake(
    {
      device_id: String(deviceId || 'web-client').trim() || 'web-client',
      cursor: asInt(cursor, 0),
    },
    userId,
  )

  return {
    session,
    state: {
      sinceVersion: asInt(cursor, 0),
      cursor: asInt(session?.cursor, asInt(cursor, 0)),
    },
  }
}

export async function pullIncremental({ userId, sinceVersion, cursor, listLimit = 50, deltaLimit = 100 }) {
  const startSince = asInt(sinceVersion, 0)
  const startCursor = asInt(cursor, 0)

  const itemsRes = await listItems(
    {
      since_version: startSince,
      limit: asInt(listLimit, 50),
      cursor: startCursor,
    },
    userId,
  )

  const deltaRes = await delta(
    {
      since_version: startSince,
      limit: asInt(deltaLimit, 100),
      cursor: startCursor,
    },
    userId,
  )

  const latestVersion = asInt(itemsRes?.latest_version, startSince)
  const nextCursor = Math.max(
    startCursor,
    asInt(itemsRes?.next_cursor, startCursor),
    asInt(deltaRes?.next_cursor, startCursor),
    latestVersion,
  )

  return {
    itemsRes,
    deltaRes,
    state: {
      sinceVersion: Math.max(startSince, latestVersion),
      cursor: nextCursor,
    },
  }
}
