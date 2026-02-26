package http

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloud-sync/internal/config"
	"cloud-sync/internal/handlers"
	"cloud-sync/internal/repos"
	"cloud-sync/internal/services"
	_ "modernc.org/sqlite"
)

func setupRouter(t *testing.T) http.Handler {
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

	repo := repos.NewSyncRepo(db)
	svc := services.NewSyncService(repo)
	h := handlers.NewSyncHandler(svc)
	cfg := config.Config{}
	return NewRouter(cfg, h)
}

func TestAPIFlow(t *testing.T) {
	r := setupRouter(t)

	handshakeReq := httptest.NewRequest(http.MethodPost, "/api/cloud-sync/v1/handshake", strings.NewReader(`{"device_id":"d1","cursor":0}`))
	handshakeReq.Header.Set("Content-Type", "application/json")
	handshakeReq.Header.Set("X-User-ID", "u1")
	handshakeRec := httptest.NewRecorder()
	r.ServeHTTP(handshakeRec, handshakeReq)
	if handshakeRec.Code != http.StatusOK {
		t.Fatalf("handshake status=%d body=%s", handshakeRec.Code, handshakeRec.Body.String())
	}

	upsertReq := httptest.NewRequest(http.MethodPost, "/api/cloud-sync/v1/items", strings.NewReader(`{"path":"/file1","metadata":{"a":1}}`))
	upsertReq.Header.Set("Content-Type", "application/json")
	upsertReq.Header.Set("X-User-ID", "u1")
	upsertRec := httptest.NewRecorder()
	r.ServeHTTP(upsertRec, upsertReq)
	if upsertRec.Code != http.StatusOK {
		t.Fatalf("upsert status=%d body=%s", upsertRec.Code, upsertRec.Body.String())
	}
	var upsertBody map[string]any
	_ = json.Unmarshal(upsertRec.Body.Bytes(), &upsertBody)
	id, _ := upsertBody["id"].(string)
	if id == "" {
		t.Fatalf("expected id in upsert response: %s", upsertRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/cloud-sync/v1/items?since_version=0&limit=10", nil)
	listReq.Header.Set("X-User-ID", "u1")
	listRec := httptest.NewRecorder()
	r.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}

	deltaReq := httptest.NewRequest(http.MethodPost, "/api/cloud-sync/v1/delta", strings.NewReader(`{"since_version":0,"limit":10}`))
	deltaReq.Header.Set("Content-Type", "application/json")
	deltaReq.Header.Set("X-User-ID", "u1")
	deltaRec := httptest.NewRecorder()
	r.ServeHTTP(deltaRec, deltaReq)
	if deltaRec.Code != http.StatusOK {
		t.Fatalf("delta status=%d body=%s", deltaRec.Code, deltaRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodPost, "/api/cloud-sync/v1/items/"+id+"/delete", strings.NewReader(`{}`))
	deleteReq.Header.Set("Content-Type", "application/json")
	deleteReq.Header.Set("X-User-ID", "u1")
	deleteRec := httptest.NewRecorder()
	r.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	restoreReq := httptest.NewRequest(http.MethodPost, "/api/cloud-sync/v1/items/"+id+"/restore", strings.NewReader(`{}`))
	restoreReq.Header.Set("Content-Type", "application/json")
	restoreReq.Header.Set("X-User-ID", "u1")
	restoreRec := httptest.NewRecorder()
	r.ServeHTTP(restoreRec, restoreReq)
	if restoreRec.Code != http.StatusOK {
		t.Fatalf("restore status=%d body=%s", restoreRec.Code, restoreRec.Body.String())
	}
}

func TestConflictResponse409(t *testing.T) {
	r := setupRouter(t)

	req1 := httptest.NewRequest(http.MethodPost, "/api/cloud-sync/v1/items", strings.NewReader(`{"path":"/c","metadata":{"v":1}}`))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("X-User-ID", "u1")
	rec1 := httptest.NewRecorder()
	r.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first upsert failed: %s", rec1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/cloud-sync/v1/items", strings.NewReader(`{"path":"/c","metadata":{"v":2},"base_version":1}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-User-ID", "u1")
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second upsert failed: %s", rec2.Body.String())
	}

	req3 := httptest.NewRequest(http.MethodPost, "/api/cloud-sync/v1/items", strings.NewReader(`{"path":"/c","metadata":{"v":3},"base_version":1}`))
	req3.Header.Set("Content-Type", "application/json")
	req3.Header.Set("X-User-ID", "u1")
	rec3 := httptest.NewRecorder()
	r.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", rec3.Code, rec3.Body.String())
	}
}
