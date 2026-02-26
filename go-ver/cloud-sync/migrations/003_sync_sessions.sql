CREATE TABLE IF NOT EXISTS sync_sessions (
  session_id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  device_id TEXT NOT NULL,
  cursor_version INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL,
  last_seen_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sync_sessions_user_device ON sync_sessions(user_id, device_id);
