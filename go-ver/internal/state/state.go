package state

import (
	"net/http"
	"sync"
	"time"

	"deepseek2api-go/internal/accounts"
	"deepseek2api-go/internal/clients"
	"deepseek2api-go/internal/config"
	"deepseek2api-go/internal/logging"
	"deepseek2api-go/internal/pow"
)

type SyncStatus struct {
	Enabled         bool   `json:"enabled"`
	Connected       bool   `json:"connected"`
	LastSuccessUnix int64  `json:"last_success_unix"`
	LastVersion     int64  `json:"last_version"`
	LastCursor      int64  `json:"last_cursor"`
	LastError       string `json:"last_error"`
}

type AppState struct {
	mu sync.RWMutex

	cfg config.Config

	Logger    *logging.Logger
	HTTP      *http.Client
	Pool      *accounts.Pool
	PowSolver pow.Solver
	PowCache  *pow.Cache
	DeepSeek  *clients.DeepSeekClient

	Sync any

	syncStatus SyncStatus
}

func NewAppState(cfg config.Config, logger *logging.Logger, httpClient *http.Client, pool *accounts.Pool, solver pow.Solver, cache *pow.Cache, ds *clients.DeepSeekClient) *AppState {
	return &AppState{
		cfg:       cfg,
		Logger:    logger,
		HTTP:      httpClient,
		Pool:      pool,
		PowSolver: solver,
		PowCache:  cache,
		DeepSeek:  ds,
		syncStatus: SyncStatus{
			Enabled: cfg.CloudSync.Enabled,
		},
	}
}

func (s *AppState) GetConfig() config.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg := s.cfg
	cfg.ClaudeModelMapping = copyStringMap(cfg.ClaudeModelMapping)
	return cfg
}

func (s *AppState) UpdateSyncRuntime(refresh bool, maxActiveAccounts int, mapping map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.Refresh = refresh
	s.cfg.MaxActiveAccounts = maxActiveAccounts
	s.cfg.ClaudeModelMapping = copyStringMap(mapping)
}

func (s *AppState) MarkSyncSuccess(version, cursor int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncStatus.Connected = true
	s.syncStatus.LastError = ""
	s.syncStatus.LastVersion = version
	s.syncStatus.LastCursor = cursor
	s.syncStatus.LastSuccessUnix = time.Now().Unix()
}

func (s *AppState) MarkSyncError(err string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncStatus.Connected = false
	s.syncStatus.LastError = err
}

func (s *AppState) SyncStatusSnapshot() SyncStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.syncStatus
}

func (s *AppState) SetSyncEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncStatus.Enabled = enabled
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
