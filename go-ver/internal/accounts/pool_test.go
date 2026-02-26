package accounts

import (
	"testing"

	"deepseek2api-go/internal/config"
)

func TestReloadCleansStaleActiveSessions(t *testing.T) {
	cfg := config.Config{
		Accounts: []config.AccountConfig{
			{Email: "a@example.com", Token: "t1"},
			{Email: "b@example.com", Token: "t2"},
		},
		Refresh:           false,
		MaxActiveAccounts: 2,
		DeepSeekHost:      "chat.deepseek.com",
	}
	p := NewPool(cfg, nil)

	ac, ok := p.Acquire(nil)
	if !ok || ac == nil {
		t.Fatalf("expected acquire success")
	}

	before := p.GetStatus()
	if got := before["active_sessions"]; got != 1 {
		t.Fatalf("expected active_sessions=1 before reload, got %v", got)
	}

	p.Reload([]config.AccountConfig{{Email: "c@example.com", Token: "t3"}}, false, 1)

	after := p.GetStatus()
	if got := after["total"]; got != 1 {
		t.Fatalf("expected total=1 after reload, got %v", got)
	}
	if got := after["max_accounts"]; got != 1 {
		t.Fatalf("expected max_accounts=1 after reload, got %v", got)
	}
	if got := after["in_use"]; got != 0 {
		t.Fatalf("expected in_use=0 after stale account removal, got %v", got)
	}
	if got := after["active_sessions"]; got != 0 {
		t.Fatalf("expected active_sessions=0 after stale account removal, got %v", got)
	}

	ac2, ok := p.Acquire(nil)
	if !ok || ac2 == nil {
		t.Fatalf("expected acquire success after reload")
	}
	if id := p.AccountID(*ac2); id != "c@example.com" {
		t.Fatalf("expected only reloaded account to be used, got %q", id)
	}
}

func TestReloadAppliesMaxActiveAccountLimit(t *testing.T) {
	cfg := config.Config{
		Accounts: []config.AccountConfig{
			{Email: "a@example.com", Token: "t1"},
			{Email: "b@example.com", Token: "t2"},
			{Email: "c@example.com", Token: "t3"},
		},
		Refresh:           false,
		MaxActiveAccounts: 3,
		DeepSeekHost:      "chat.deepseek.com",
	}
	p := NewPool(cfg, nil)

	p.Reload(cfg.Accounts, false, 1)

	status := p.GetStatus()
	if got := status["total"]; got != 1 {
		t.Fatalf("expected total=1 after max limit applied, got %v", got)
	}
	if got := status["max_accounts"]; got != 1 {
		t.Fatalf("expected max_accounts=1, got %v", got)
	}
	if got := status["available"]; got != 1 {
		t.Fatalf("expected available=1, got %v", got)
	}
}
