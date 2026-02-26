package cloudsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"deepseek2api-go/internal/accounts"
	"deepseek2api-go/internal/config"
	"deepseek2api-go/internal/logging"
	"deepseek2api-go/internal/state"
)

func TestUpsertWithConflictRetryResolves(t *testing.T) {
	var upsertCalls int32
	var resolveCalls int32
	var listCalls int32
	var deltaCalls int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/items":
			atomic.AddInt32(&listCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items":          []any{},
				"next_cursor":    0,
				"latest_version": 7,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/delta":
			atomic.AddInt32(&deltaCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"events":      []any{},
				"next_cursor": 0,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/items":
			calls := atomic.AddInt32(&upsertCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			if calls == 1 {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":          "conflict",
					"server_version": 7,
					"server_hash":    "hash-7",
				})
				return
			}
			t.Fatalf("unexpected second /items call")
		case r.Method == http.MethodPost && r.URL.Path == "/conflict/resolve":
			atomic.AddInt32(&resolveCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":       "item-1",
				"path":     accountsPath,
				"metadata": map[string]any{"accounts": []any{}},
				"version":  8,
				"hash":     "hash-8",
				"deleted":  false,
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer ts.Close()

	cfg := config.Config{
		Accounts: []config.AccountConfig{{Email: "a@example.com", Token: "t"}},
		Refresh:  false,
		ClaudeModelMapping: map[string]string{
			"fast": "deepseek-chat",
			"slow": "deepseek-chat",
		},
		MaxActiveAccounts: 1,
		DeepSeekHost:      "chat.deepseek.com",
		CloudSync: config.CloudSyncConfig{
			Enabled: true,
		},
	}

	httpClient := ts.Client()
	pool := accounts.NewPool(cfg, httpClient)
	st := state.NewAppState(cfg, logging.New("error"), httpClient, pool, nil, nil, nil)
	m := NewSyncManager(st, NewClient(httpClient, ts.URL, "", "u1"), config.CloudSyncConfig{Limit: 100, IntervalSeconds: 1, DeviceID: "d1"})

	err := m.upsertWithConflictRetry(context.Background(), accountsPath, map[string]any{"accounts": []any{}})
	if err != nil {
		t.Fatalf("upsertWithConflictRetry error: %v", err)
	}
	if got := atomic.LoadInt32(&upsertCalls); got != 1 {
		t.Fatalf("expected 1 upsert call, got %d", got)
	}
	if got := atomic.LoadInt32(&resolveCalls); got != 1 {
		t.Fatalf("expected 1 resolve call, got %d", got)
	}
	if got := atomic.LoadInt32(&listCalls); got < 1 {
		t.Fatalf("expected at least 1 list call during conflict recovery, got %d", got)
	}
	if got := atomic.LoadInt32(&deltaCalls); got < 1 {
		t.Fatalf("expected at least 1 delta call during conflict recovery, got %d", got)
	}
	if v := m.getVersion(); v != 8 {
		t.Fatalf("expected version=8 after resolve, got %d", v)
	}
	if c := m.getCursor(); c != 8 {
		t.Fatalf("expected cursor=8 after resolve, got %d", c)
	}
}

func TestApplyItemsUpdatesRuntimeAndPool(t *testing.T) {
	cfg := config.Config{
		Accounts: []config.AccountConfig{{Email: "old@example.com", Token: "t-old"}},
		Refresh:  false,
		ClaudeModelMapping: map[string]string{
			"fast": "deepseek-chat",
			"slow": "deepseek-chat",
		},
		MaxActiveAccounts: 1,
		DeepSeekHost:      "chat.deepseek.com",
		CloudSync: config.CloudSyncConfig{
			Enabled: true,
		},
	}
	pool := accounts.NewPool(cfg, &http.Client{})
	st := state.NewAppState(cfg, logging.New("error"), &http.Client{}, pool, nil, nil, nil)
	m := NewSyncManager(st, nil, config.CloudSyncConfig{Limit: 100, IntervalSeconds: 1})

	items := []SyncItem{
		{
			Path:    configPath,
			Version: 10,
			Metadata: map[string]any{
				"refresh":             true,
				"max_active_accounts": 2,
				"claude_model_mapping": map[string]any{
					"fast": "deepseek-reasoner",
					"slow": "deepseek-chat",
				},
			},
		},
		{
			Path:    accountsPath,
			Version: 11,
			Metadata: map[string]any{
				"accounts": []any{
					map[string]any{"email": "new1@example.com", "token": "t1"},
					map[string]any{"email": "new2@example.com", "token": "t2"},
				},
			},
		},
	}

	if err := m.applyItems(items); err != nil {
		t.Fatalf("applyItems error: %v", err)
	}

	gotCfg := st.GetConfig()
	if !gotCfg.Refresh {
		t.Fatalf("expected refresh=true after apply")
	}
	if gotCfg.MaxActiveAccounts != 2 {
		t.Fatalf("expected max_active_accounts=2, got %d", gotCfg.MaxActiveAccounts)
	}
	if gotCfg.ClaudeModelMapping["fast"] != "deepseek-reasoner" {
		t.Fatalf("expected fast mapping updated, got %q", gotCfg.ClaudeModelMapping["fast"])
	}

	status := st.Pool.GetStatus()
	if got := status["total"]; got != 2 {
		t.Fatalf("expected pool total=2, got %v", got)
	}
	if got := status["max_accounts"]; got != 2 {
		t.Fatalf("expected pool max_accounts=2, got %v", got)
	}
	if v := m.getVersion(); v != 11 {
		t.Fatalf("expected manager version=11, got %d", v)
	}
}
