package services

import (
	"database/sql"
	"encoding/json"
	"testing"

	"cloud-sync/internal/repos"
	_ "modernc.org/sqlite"
)

func setupTestService(t *testing.T) *SyncService {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	stmts := []string{
		`CREATE TABLE sync_items (
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
		);`,
		`CREATE TABLE sync_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id TEXT NOT NULL,
			item_id TEXT NOT NULL,
			path TEXT NOT NULL,
			event_type TEXT NOT NULL,
			version INTEGER NOT NULL,
			metadata TEXT NOT NULL DEFAULT '{}',
			hash TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,
		`CREATE TABLE sync_sessions (
			session_id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			device_id TEXT NOT NULL,
			cursor_version INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			last_seen_at DATETIME NOT NULL
		);`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}

	return NewSyncService(repos.NewSyncRepo(db))
}

func TestVersionMonotonicAndConflict(t *testing.T) {
	svc := setupTestService(t)
	user := "u1"

	i1, err := svc.Upsert(user, UpsertInput{Path: "/a", Metadata: json.RawMessage(`{"v":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if i1.Version != 1 {
		t.Fatalf("expected version 1, got %d", i1.Version)
	}

	base := i1.Version
	i2, err := svc.Upsert(user, UpsertInput{Path: "/a", Metadata: json.RawMessage(`{"v":2}`), BaseVersion: &base})
	if err != nil {
		t.Fatal(err)
	}
	if i2.Version != 2 {
		t.Fatalf("expected version 2, got %d", i2.Version)
	}

	stale := int64(1)
	_, err = svc.Upsert(user, UpsertInput{Path: "/a", Metadata: json.RawMessage(`{"v":3}`), BaseVersion: &stale})
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if _, ok := err.(*ConflictError); !ok {
		t.Fatalf("expected ConflictError, got %T", err)
	}
}

func TestDeleteRestoreSemantics(t *testing.T) {
	svc := setupTestService(t)
	user := "u2"

	item, err := svc.Upsert(user, UpsertInput{Path: "/b", Metadata: json.RawMessage(`{"v":1}`)})
	if err != nil {
		t.Fatal(err)
	}

	del, err := svc.Delete(user, item.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !del.Deleted {
		t.Fatal("expected deleted=true")
	}

	res, err := svc.Restore(user, item.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted {
		t.Fatal("expected deleted=false")
	}
	if res.Version <= del.Version {
		t.Fatal("expected version to increase after restore")
	}
}
