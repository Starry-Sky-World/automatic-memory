package services

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cloud-sync/internal/models"
	"cloud-sync/internal/repos"
)

var (
	ErrConflict = errors.New("version conflict")
)

type ConflictError struct {
	ServerVersion int64  `json:"server_version"`
	ServerHash    string `json:"server_hash"`
}

func (e *ConflictError) Error() string {
	return ErrConflict.Error()
}

type UpsertInput struct {
	Path        string          `json:"path"`
	Metadata    json.RawMessage `json:"metadata"`
	BaseVersion *int64          `json:"base_version"`
	Content     []byte          `json:"-"`
}

type ListItemsInput struct {
	SinceVersion int64
	Limit        int
	Cursor       int64
}

type DeltaInput struct {
	SinceVersion int64 `json:"since_version"`
	Limit        int   `json:"limit"`
	Cursor       int64 `json:"cursor"`
}

type HandshakeInput struct {
	DeviceID string `json:"device_id"`
	Cursor   int64  `json:"cursor"`
}

type ResolveConflictInput struct {
	ID          string          `json:"id"`
	Path        string          `json:"path"`
	Metadata    json.RawMessage `json:"metadata"`
	BaseVersion int64           `json:"base_version"`
	Content     []byte          `json:"-"`
}

type SyncService struct {
	repo *repos.SyncRepo
}

func NewSyncService(repo *repos.SyncRepo) *SyncService {
	return &SyncService{repo: repo}
}

func (s *SyncService) Upsert(userID string, in UpsertInput) (*models.SyncItem, error) {
	path := strings.TrimSpace(in.Path)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	meta := normalizeMetadata(in.Metadata)
	hash := computeHash(path, meta, in.Content)

	var out *models.SyncItem
	err := s.repo.WithTx(func(tx *sql.Tx) error {
		existing, err := s.repo.GetItemByPathTx(tx, userID, path)
		if err != nil && !errors.Is(err, repos.ErrNotFound) {
			return err
		}
		if existing != nil && in.BaseVersion != nil && *in.BaseVersion != existing.Version {
			return &ConflictError{ServerVersion: existing.Version, ServerHash: existing.Hash}
		}

		nextVersion, err := s.repo.NextVersionTx(tx, userID)
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		item := &models.SyncItem{
			UserID:    userID,
			Path:      path,
			Metadata:  string(meta),
			Version:   nextVersion,
			Hash:      hash,
			Deleted:   false,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if existing != nil {
			item.ID = existing.ID
			item.CreatedAt = existing.CreatedAt
		} else {
			item.ID = newItemID(userID, path, now.UnixNano())
		}
		if err := s.repo.UpsertItemTx(tx, item); err != nil {
			return err
		}
		event := &models.SyncEvent{
			UserID:    userID,
			ItemID:    item.ID,
			Path:      item.Path,
			Type:      "upsert",
			Version:   item.Version,
			Metadata:  item.Metadata,
			Hash:      item.Hash,
			CreatedAt: now,
		}
		if err := s.repo.InsertEventTx(tx, event); err != nil {
			return err
		}
		out = item
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SyncService) GetItem(userID, id string) (*models.SyncItem, error) {
	return s.repo.GetItemByID(userID, id)
}

func (s *SyncService) ListItems(userID string, in ListItemsInput) ([]models.SyncItem, int64, int64, error) {
	items, nextCursor, err := s.repo.ListItems(userID, in.SinceVersion, in.Limit, in.Cursor)
	if err != nil {
		return nil, 0, 0, err
	}
	latest, err := s.repo.LatestVersion(userID)
	if err != nil {
		return nil, 0, 0, err
	}
	return items, nextCursor, latest, nil
}

func (s *SyncService) Delete(userID, id string, baseVersion *int64) (*models.SyncItem, error) {
	return s.setDeleteState(userID, id, true, "delete", baseVersion)
}

func (s *SyncService) Restore(userID, id string, baseVersion *int64) (*models.SyncItem, error) {
	return s.setDeleteState(userID, id, false, "restore", baseVersion)
}

func (s *SyncService) setDeleteState(userID, id string, deleted bool, evtType string, baseVersion *int64) (*models.SyncItem, error) {
	var out *models.SyncItem
	err := s.repo.WithTx(func(tx *sql.Tx) error {
		item, err := s.repo.GetItemByIDTx(tx, userID, strings.TrimSpace(id))
		if err != nil {
			return err
		}
		if baseVersion != nil && *baseVersion != item.Version {
			return &ConflictError{ServerVersion: item.Version, ServerHash: item.Hash}
		}
		nextVersion, err := s.repo.NextVersionTx(tx, userID)
		if err != nil {
			return err
		}
		item.Version = nextVersion
		item.Deleted = deleted
		item.UpdatedAt = time.Now().UTC()
		if err := s.repo.UpsertItemTx(tx, item); err != nil {
			return err
		}
		evt := &models.SyncEvent{
			UserID:    userID,
			ItemID:    item.ID,
			Path:      item.Path,
			Type:      evtType,
			Version:   item.Version,
			Metadata:  item.Metadata,
			Hash:      item.Hash,
			CreatedAt: item.UpdatedAt,
		}
		if err := s.repo.InsertEventTx(tx, evt); err != nil {
			return err
		}
		out = item
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SyncService) Delta(userID string, in DeltaInput) ([]models.SyncEvent, int64, error) {
	return s.repo.ListEvents(userID, in.SinceVersion, in.Limit, in.Cursor)
}

func (s *SyncService) Handshake(userID string, in HandshakeInput) (*models.SyncSession, error) {
	if strings.TrimSpace(in.DeviceID) == "" {
		return nil, fmt.Errorf("device_id is required")
	}
	return s.repo.UpsertSession(userID, strings.TrimSpace(in.DeviceID), in.Cursor)
}

func (s *SyncService) ResolveConflict(userID string, in ResolveConflictInput) (*models.SyncItem, error) {
	if strings.TrimSpace(in.ID) == "" {
		in.Path = strings.TrimSpace(in.Path)
		if in.Path == "" {
			return nil, fmt.Errorf("id or path is required")
		}
	}
	base := in.BaseVersion
	if strings.TrimSpace(in.ID) != "" {
		item, err := s.GetItem(userID, strings.TrimSpace(in.ID))
		if err != nil {
			return nil, err
		}
		base = item.Version
		in.Path = item.Path
	}
	return s.Upsert(userID, UpsertInput{
		Path:        in.Path,
		Metadata:    in.Metadata,
		BaseVersion: &base,
		Content:     in.Content,
	})
}

func normalizeMetadata(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("{}")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return []byte("{}")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func computeHash(path string, metadata []byte, content []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte(path))
	_, _ = h.Write(metadata)
	_, _ = h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

func newItemID(userID, path string, nonce int64) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", userID, path, nonce)))
	return hex.EncodeToString(h[:16])
}
