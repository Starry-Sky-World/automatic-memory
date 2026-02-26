package cloudsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"deepseek2api-go/internal/config"
	"deepseek2api-go/internal/state"
)

const (
	accountsPath = "/deepseek2api/accounts"
	configPath   = "/deepseek2api/config"
)

type SyncConfigPayload struct {
	Refresh            bool              `json:"refresh"`
	MaxActiveAccounts  int               `json:"max_active_accounts"`
	ClaudeModelMapping map[string]string `json:"claude_model_mapping"`
}

type SyncManager struct {
	mu sync.Mutex

	st     *state.AppState
	client *Client
	cfg    config.CloudSyncConfig

	version int64
	cursor  int64
}

func NewSyncManager(st *state.AppState, client *Client, cfg config.CloudSyncConfig) *SyncManager {
	return &SyncManager{st: st, client: client, cfg: cfg}
}

func (m *SyncManager) InitialSync(ctx context.Context) error {
	s, err := m.client.Handshake(ctx, HandshakeRequest{DeviceID: m.cfg.DeviceID, Cursor: m.cursor})
	if err != nil {
		m.st.MarkSyncError(err.Error())
		return err
	}
	m.mu.Lock()
	if s != nil {
		m.cursor = s.Cursor
	}
	m.mu.Unlock()

	if err := m.pullAndApply(ctx); err != nil {
		m.st.MarkSyncError(err.Error())
		return err
	}
	if err := m.pushLocalSnapshot(ctx); err != nil {
		m.st.MarkSyncError(err.Error())
		return err
	}
	m.st.MarkSyncSuccess(m.getVersion(), m.getCursor())
	return nil
}

func (m *SyncManager) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(m.cfg.IntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.SyncOnce(ctx); err != nil {
				m.st.MarkSyncError(err.Error())
			} else {
				m.st.MarkSyncSuccess(m.getVersion(), m.getCursor())
			}
		}
	}
}

func (m *SyncManager) SyncOnce(ctx context.Context) error {
	if err := m.pullAndApply(ctx); err != nil {
		return err
	}
	return m.pushLocalSnapshot(ctx)
}

func (m *SyncManager) pullAndApply(ctx context.Context) error {
	m.mu.Lock()
	since := m.version
	cursor := m.cursor
	limit := m.cfg.Limit
	m.mu.Unlock()

	listResp, err := m.client.ListItems(ctx, since, limit, cursor)
	if err != nil {
		return err
	}
	if listResp == nil {
		return nil
	}
	if err := m.applyItems(listResp.Items); err != nil {
		return err
	}

	deltaResp, err := m.client.Delta(ctx, DeltaRequest{SinceVersion: since, Limit: limit, Cursor: cursor})
	if err != nil {
		return err
	}
	maxVersion := listResp.LatestVersion
	if listResp.NextCursor > cursor {
		cursor = listResp.NextCursor
	}
	if deltaResp != nil && deltaResp.NextCursor > cursor {
		cursor = deltaResp.NextCursor
	}

	m.mu.Lock()
	if maxVersion > m.version {
		m.version = maxVersion
	}
	if cursor > m.cursor {
		m.cursor = cursor
	}
	m.mu.Unlock()
	return nil
}

func (m *SyncManager) pushLocalSnapshot(ctx context.Context) error {
	cfg := m.st.GetConfig()
	accounts := m.st.Pool.SnapshotConfigAccounts()

	accountsMeta := map[string]any{"accounts": accounts}
	configMeta := SyncConfigPayload{
		Refresh:            cfg.Refresh,
		MaxActiveAccounts:  cfg.MaxActiveAccounts,
		ClaudeModelMapping: cfg.ClaudeModelMapping,
	}
	if err := m.upsertWithConflictRetry(ctx, accountsPath, accountsMeta); err != nil {
		return err
	}
	if err := m.upsertWithConflictRetry(ctx, configPath, configMeta); err != nil {
		return err
	}
	return nil
}

func (m *SyncManager) upsertWithConflictRetry(ctx context.Context, path string, metadata any) error {
	base := m.getVersionPtr()
	item, err := m.client.UpsertItem(ctx, UpsertRequest{Path: path, Metadata: metadata, BaseVersion: base})
	if err == nil {
		m.advanceVersionCursor(item)
		return nil
	}
	var ce *ConflictError
	if !errors.As(err, &ce) {
		return err
	}
	if pullErr := m.pullAndApply(ctx); pullErr != nil {
		return pullErr
	}
	resolved, resolveErr := m.client.ResolveConflict(ctx, ResolveConflictRequest{Path: path, Metadata: metadata, BaseVersion: ce.ServerVersion})
	if resolveErr != nil {
		return resolveErr
	}
	m.advanceVersionCursor(resolved)
	return nil
}

func (m *SyncManager) applyItems(items []SyncItem) error {
	if len(items) == 0 {
		return nil
	}
	var (
		remoteAccounts []config.AccountConfig
		remoteCfg      *SyncConfigPayload
	)
	for _, item := range items {
		if item.Deleted {
			continue
		}
		switch strings.TrimSpace(item.Path) {
		case accountsPath:
			acc, err := decodeAccounts(item.Metadata)
			if err != nil {
				return err
			}
			remoteAccounts = acc
		case configPath:
			cfgPayload, err := decodeConfigPayload(item.Metadata)
			if err != nil {
				return err
			}
			remoteCfg = cfgPayload
		}
		if item.Version > m.getVersion() {
			m.setVersion(item.Version)
		}
	}
	if remoteCfg != nil {
		m.st.UpdateSyncRuntime(remoteCfg.Refresh, remoteCfg.MaxActiveAccounts, remoteCfg.ClaudeModelMapping)
	}
	if remoteCfg != nil || remoteAccounts != nil {
		cfg := m.st.GetConfig()
		accountsToApply := remoteAccounts
		if accountsToApply == nil {
			accountsToApply = m.st.Pool.SnapshotConfigAccounts()
		}
		m.st.Pool.Reload(accountsToApply, cfg.Refresh, cfg.MaxActiveAccounts)
	}
	return nil
}

func decodeAccounts(v any) ([]config.AccountConfig, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var wrapped struct {
		Accounts []config.AccountConfig `json:"accounts"`
	}
	if err := json.Unmarshal(b, &wrapped); err == nil && wrapped.Accounts != nil {
		return wrapped.Accounts, nil
	}
	var direct []config.AccountConfig
	if err := json.Unmarshal(b, &direct); err == nil {
		return direct, nil
	}
	return nil, fmt.Errorf("invalid accounts payload")
}

func decodeConfigPayload(v any) (*SyncConfigPayload, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out SyncConfigPayload
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	if out.ClaudeModelMapping == nil {
		out.ClaudeModelMapping = map[string]string{"fast": "deepseek-chat", "slow": "deepseek-chat"}
	}
	return &out, nil
}

func (m *SyncManager) getVersionPtr() *int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := m.version
	return &v
}

func (m *SyncManager) getVersion() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.version
}

func (m *SyncManager) setVersion(v int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v > m.version {
		m.version = v
	}
}

func (m *SyncManager) getCursor() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cursor
}

func (m *SyncManager) advanceVersionCursor(item *SyncItem) {
	if item == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if item.Version > m.version {
		m.version = item.Version
	}
	if item.Version > m.cursor {
		m.cursor = item.Version
	}
}
