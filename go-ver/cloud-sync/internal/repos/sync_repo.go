package repos

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"cloud-sync/internal/models"
)

var ErrNotFound = errors.New("not found")

type SyncRepo struct {
	db *sql.DB
}

func NewSyncRepo(db *sql.DB) *SyncRepo {
	return &SyncRepo{db: db}
}

func (r *SyncRepo) DB() *sql.DB {
	return r.db
}

func (r *SyncRepo) WithTx(fn func(tx *sql.Tx) error) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (r *SyncRepo) NextVersionTx(tx *sql.Tx, userID string) (int64, error) {
	var next int64
	err := tx.QueryRow(`SELECT COALESCE(MAX(version), 0) + 1 FROM sync_events WHERE user_id = ?`, userID).Scan(&next)
	return next, err
}

func (r *SyncRepo) GetItemByPathTx(tx *sql.Tx, userID, path string) (*models.SyncItem, error) {
	row := tx.QueryRow(`
		SELECT id, user_id, path, metadata, version, hash, deleted, created_at, updated_at
		FROM sync_items WHERE user_id = ? AND path = ?
	`, userID, path)
	return scanItem(row)
}

func (r *SyncRepo) GetItemByIDTx(tx *sql.Tx, userID, id string) (*models.SyncItem, error) {
	row := tx.QueryRow(`
		SELECT id, user_id, path, metadata, version, hash, deleted, created_at, updated_at
		FROM sync_items WHERE user_id = ? AND id = ?
	`, userID, id)
	return scanItem(row)
}

func (r *SyncRepo) GetItemByID(userID, id string) (*models.SyncItem, error) {
	row := r.db.QueryRow(`
		SELECT id, user_id, path, metadata, version, hash, deleted, created_at, updated_at
		FROM sync_items WHERE user_id = ? AND id = ?
	`, userID, id)
	return scanItem(row)
}

func (r *SyncRepo) UpsertItemTx(tx *sql.Tx, item *models.SyncItem) error {
	_, err := tx.Exec(`
		INSERT INTO sync_items (id, user_id, path, metadata, version, hash, deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, path) DO UPDATE SET
			metadata = excluded.metadata,
			version = excluded.version,
			hash = excluded.hash,
			deleted = excluded.deleted,
			updated_at = excluded.updated_at
	`, item.ID, item.UserID, item.Path, item.Metadata, item.Version, item.Hash, item.Deleted, item.CreatedAt.UTC(), item.UpdatedAt.UTC())
	return err
}

func (r *SyncRepo) InsertEventTx(tx *sql.Tx, evt *models.SyncEvent) error {
	res, err := tx.Exec(`
		INSERT INTO sync_events (user_id, item_id, path, event_type, version, metadata, hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, evt.UserID, evt.ItemID, evt.Path, evt.Type, evt.Version, evt.Metadata, evt.Hash, evt.CreatedAt.UTC())
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err == nil {
		evt.ID = id
	}
	return nil
}

func (r *SyncRepo) ListItems(userID string, sinceVersion int64, limit int, cursorVersion int64) ([]models.SyncItem, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	effectiveSince := sinceVersion
	if cursorVersion > effectiveSince {
		effectiveSince = cursorVersion
	}
	rows, err := r.db.Query(`
		SELECT id, user_id, path, metadata, version, hash, deleted, created_at, updated_at
		FROM sync_items
		WHERE user_id = ? AND version > ?
		ORDER BY version ASC
		LIMIT ?
	`, userID, effectiveSince, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]models.SyncItem, 0, limit)
	var nextCursor int64
	for rows.Next() {
		it, err := scanItemFromRows(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *it)
		nextCursor = it.Version
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(items) == 0 {
		nextCursor = effectiveSince
	}
	return items, nextCursor, nil
}

func (r *SyncRepo) LatestVersion(userID string) (int64, error) {
	var v int64
	err := r.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM sync_events WHERE user_id = ?`, userID).Scan(&v)
	return v, err
}

func (r *SyncRepo) ListEvents(userID string, sinceVersion int64, limit int, cursorVersion int64) ([]models.SyncEvent, int64, error) {
	if limit <= 0 {
		limit = 100
	}
	effectiveSince := sinceVersion
	if cursorVersion > effectiveSince {
		effectiveSince = cursorVersion
	}
	rows, err := r.db.Query(`
		SELECT id, user_id, item_id, path, event_type, version, metadata, hash, created_at
		FROM sync_events
		WHERE user_id = ? AND version > ?
		ORDER BY version ASC
		LIMIT ?
	`, userID, effectiveSince, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	events := make([]models.SyncEvent, 0, limit)
	var nextCursor int64
	for rows.Next() {
		var e models.SyncEvent
		if err := rows.Scan(&e.ID, &e.UserID, &e.ItemID, &e.Path, &e.Type, &e.Version, &e.Metadata, &e.Hash, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		events = append(events, e)
		nextCursor = e.Version
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(events) == 0 {
		nextCursor = effectiveSince
	}
	return events, nextCursor, nil
}

func (r *SyncRepo) UpsertSession(userID, deviceID string, cursor int64) (*models.SyncSession, error) {
	now := time.Now().UTC()
	sessionID := fmt.Sprintf("%s:%s", userID, deviceID)
	_, err := r.db.Exec(`
		INSERT INTO sync_sessions (session_id, user_id, device_id, cursor_version, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			cursor_version = excluded.cursor_version,
			last_seen_at = excluded.last_seen_at
	`, sessionID, userID, deviceID, cursor, now, now)
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRow(`
		SELECT session_id, user_id, device_id, cursor_version, created_at, last_seen_at
		FROM sync_sessions WHERE session_id = ?
	`, sessionID)
	var s models.SyncSession
	if err := row.Scan(&s.SessionID, &s.UserID, &s.DeviceID, &s.CursorVersion, &s.CreatedAt, &s.LastSeenAt); err != nil {
		return nil, err
	}
	return &s, nil
}

func scanItem(row interface{ Scan(dest ...any) error }) (*models.SyncItem, error) {
	var it models.SyncItem
	if err := row.Scan(&it.ID, &it.UserID, &it.Path, &it.Metadata, &it.Version, &it.Hash, &it.Deleted, &it.CreatedAt, &it.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &it, nil
}

func scanItemFromRows(rows *sql.Rows) (*models.SyncItem, error) {
	var it models.SyncItem
	if err := rows.Scan(&it.ID, &it.UserID, &it.Path, &it.Metadata, &it.Version, &it.Hash, &it.Deleted, &it.CreatedAt, &it.UpdatedAt); err != nil {
		return nil, err
	}
	return &it, nil
}
