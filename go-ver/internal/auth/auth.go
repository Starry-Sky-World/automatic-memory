package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"deepseek2api-go/internal/accounts"
	"deepseek2api-go/internal/config"
)

type ctxKey string

const AuthContextKey ctxKey = "auth_ctx"

type AuthContext struct {
	UseConfigToken bool
	CallerKey      string
	DeepSeekToken  string
	Account        *accounts.Account
	FailedAccounts map[string]bool
	Released       bool
}

func fromContext(r *http.Request) *AuthContext {
	v := r.Context().Value(AuthContextKey)
	if ac, ok := v.(*AuthContext); ok && ac != nil {
		return ac
	}
	return nil
}

func WithAuthContext(r *http.Request, ac *AuthContext) *http.Request {
	ctx := context.WithValue(r.Context(), AuthContextKey, ac)
	return r.WithContext(ctx)
}

func DetermineModeAndToken(r *http.Request, cfg config.Config, pool *accounts.Pool) (*AuthContext, int, string, error) {
	callerKey := strings.TrimSpace(r.Header.Get("X-OA-Key"))
	if callerKey == "" {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			callerKey = strings.TrimSpace(auth[7:])
		}
	}
	if callerKey == "" {
		return nil, http.StatusUnauthorized, "Unauthorized: missing X-OA-Key or Authorization Bearer header.", errors.New("missing auth")
	}
	ac := &AuthContext{CallerKey: callerKey, FailedAccounts: map[string]bool{}}
	usePool := false
	for _, k := range cfg.Keys {
		if callerKey == k {
			usePool = true
			break
		}
	}
	if !usePool {
		ac.UseConfigToken = false
		ac.DeepSeekToken = callerKey
		return ac, 0, "", nil
	}
	acc, ok := pool.Acquire(nil)
	if !ok || acc == nil {
		return nil, http.StatusTooManyRequests, "No accounts available in pool.", errors.New("no accounts")
	}
	if err := pool.EnsureToken(acc); err != nil {
		pool.Release(acc)
		return nil, http.StatusInternalServerError, "Account login failed.", err
	}
	ac.UseConfigToken = true
	ac.Account = acc
	ac.DeepSeekToken = strings.TrimSpace(acc.Token)
	return ac, 0, "", nil
}

func DetermineClaudeModeAndToken(r *http.Request, cfg config.Config, pool *accounts.Pool) (*AuthContext, int, string, error) {
	return DetermineModeAndToken(r, cfg, pool)
}

func GetAuthHeaders(cfg config.Config, ac *AuthContext) map[string]string {
	h := cfg.BaseHeaders()
	h["authorization"] = "Bearer " + ac.DeepSeekToken
	return h
}

func ReleaseAccountIfNeeded(ac *AuthContext, pool *accounts.Pool) {
	if ac == nil || !ac.UseConfigToken || ac.Released {
		return
	}
	pool.Release(ac.Account)
	ac.Released = true
	ac.Account = nil
}

func SwitchAccount(ac *AuthContext, pool *accounts.Pool) bool {
	if ac == nil || !ac.UseConfigToken {
		return false
	}
	if ac.Account != nil {
		ac.FailedAccounts[pool.AccountID(*ac.Account)] = true
		pool.Release(ac.Account)
	}
	next, ok := pool.Acquire(ac.FailedAccounts)
	if !ok || next == nil {
		ac.Account = nil
		ac.DeepSeekToken = ""
		return false
	}
	if err := pool.EnsureToken(next); err != nil {
		ac.Account = nil
		ac.DeepSeekToken = ""
		return false
	}
	ac.Account = next
	ac.DeepSeekToken = next.Token
	return true
}
