CREATE TABLE IF NOT EXISTS sync_items (
  id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  path TEXT NOT NULL,
  metadata TEXT NOT NULL DEFAULT '{}',
  version INTEGER NOT NULL,
  hash TEXT NOT NULL,
  deleted INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  PRIMARY KEY (id),
  UNIQUE(user_id, path)
);

CREATE INDEX IF NOT EXISTS idx_sync_items_user_version ON sync_items(user_id, version);
