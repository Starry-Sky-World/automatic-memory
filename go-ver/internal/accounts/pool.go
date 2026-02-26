package accounts

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"deepseek2api-go/internal/config"
)

type Account struct {
	Email    string `json:"email"`
	Mobile   string `json:"mobile"`
	Password string `json:"password"`
	Token    string `json:"token"`
}

type Pool struct {
	mu           sync.Mutex
	accounts     []Account
	active       map[string]int
	refresh      bool
	maxAccounts  int
	httpClient   *http.Client
	loginURL     string
	baseHeaders  map[string]string
	lastWarnUnix int64
}

func NewPool(cfg config.Config, httpClient *http.Client) *Pool {
	p := &Pool{active: map[string]int{}, httpClient: httpClient, loginURL: cfg.URLLogin(), baseHeaders: cfg.BaseHeaders()}
	p.reloadLocked(cfg.Accounts, cfg.Refresh, cfg.MaxActiveAccounts)
	return p
}

func (p *Pool) AccountID(a Account) string {
	if strings.TrimSpace(a.Email) != "" {
		return strings.TrimSpace(a.Email)
	}
	return strings.TrimSpace(a.Mobile)
}

func (p *Pool) Acquire(exclude map[string]bool) (*Account, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.accounts) == 0 {
		now := time.Now().Unix()
		if now-p.lastWarnUnix > 30 {
			p.lastWarnUnix = now
		}
		return nil, false
	}
	cands := make([]int, 0, len(p.accounts))
	for i := range p.accounts {
		id := p.AccountID(p.accounts[i])
		if exclude != nil && exclude[id] {
			continue
		}
		cands = append(cands, i)
	}
	if len(cands) == 0 {
		for i := range p.accounts {
			cands = append(cands, i)
		}
	}
	idx := cands[rand.Intn(len(cands))]
	id := p.AccountID(p.accounts[idx])
	p.active[id]++
	ac := p.accounts[idx]
	return &ac, true
}

func (p *Pool) Release(a *Account) {
	if a == nil {
		return
	}
	id := p.AccountID(*a)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active[id] > 1 {
		p.active[id]--
	} else {
		delete(p.active, id)
	}
}

func (p *Pool) GetStatus() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	total := len(p.accounts)
	inUse := len(p.active)
	activeSessions := 0
	for _, v := range p.active {
		activeSessions += v
	}
	return map[string]any{"total": total, "available": total - inUse, "in_use": inUse, "active_sessions": activeSessions, "max_accounts": p.maxAccounts}
}

func (p *Pool) Reload(accounts []config.AccountConfig, refresh bool, maxAccounts int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reloadLocked(accounts, refresh, maxAccounts)
}

func (p *Pool) UpdateRuntime(refresh bool, maxAccounts int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	current := p.snapshotConfigLocked()
	p.reloadLocked(current, refresh, maxAccounts)
}

func (p *Pool) SnapshotConfigAccounts() []config.AccountConfig {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snapshotConfigLocked()
}

func (p *Pool) EnsureToken(a *Account) error {
	if a == nil {
		return errors.New("nil account")
	}
	if strings.TrimSpace(a.Token) != "" && !p.refresh {
		return nil
	}
	if strings.TrimSpace(a.Password) == "" || (strings.TrimSpace(a.Email) == "" && strings.TrimSpace(a.Mobile) == "") {
		if strings.TrimSpace(a.Token) != "" {
			return nil
		}
		return errors.New("missing credentials")
	}
	payload := map[string]any{"password": a.Password, "device_id": "deepseek_to_api", "os": "android"}
	if strings.TrimSpace(a.Email) != "" {
		payload["email"] = a.Email
	} else {
		payload["mobile"] = a.Mobile
		payload["area_code"] = nil
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, p.loginURL, bytes.NewReader(b))
	for k, v := range p.baseHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Del("Accept-Encoding")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	data, _ := body["data"].(map[string]any)
	biz, _ := data["biz_data"].(map[string]any)
	user, _ := biz["user"].(map[string]any)
	tok, _ := user["token"].(string)
	if strings.TrimSpace(tok) == "" {
		return errors.New("missing token")
	}
	a.Token = tok
	return nil
}

func (p *Pool) reloadLocked(accounts []config.AccountConfig, refresh bool, maxAccounts int) {
	p.refresh = refresh
	p.accounts = make([]Account, 0, len(accounts))
	for _, a := range accounts {
		p.accounts = append(p.accounts, Account{Email: a.Email, Mobile: a.Mobile, Password: a.Password, Token: a.Token})
	}
	p.maxAccounts = maxAccounts
	if p.maxAccounts <= 0 || p.maxAccounts > len(p.accounts) {
		p.maxAccounts = len(p.accounts)
	}
	if p.maxAccounts < len(p.accounts) {
		rand.Shuffle(len(p.accounts), func(i, j int) { p.accounts[i], p.accounts[j] = p.accounts[j], p.accounts[i] })
		p.accounts = p.accounts[:p.maxAccounts]
	}
	valid := map[string]struct{}{}
	for _, a := range p.accounts {
		valid[p.AccountID(a)] = struct{}{}
	}
	for id := range p.active {
		if _, ok := valid[id]; !ok {
			delete(p.active, id)
		}
	}
}

func (p *Pool) snapshotConfigLocked() []config.AccountConfig {
	out := make([]config.AccountConfig, 0, len(p.accounts))
	for _, a := range p.accounts {
		out = append(out, config.AccountConfig{Email: a.Email, Mobile: a.Mobile, Password: a.Password, Token: a.Token})
	}
	return out
}
