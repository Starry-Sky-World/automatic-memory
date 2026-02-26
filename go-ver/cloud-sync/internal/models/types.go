package models

import "time"

type SyncItem struct {
	ID          string    `json:"id"`
	UserID      string    `json:"-"`
	Path        string    `json:"path"`
	Metadata    string    `json:"metadata"`
	Version     int64     `json:"version"`
	Hash        string    `json:"hash"`
	Deleted     bool      `json:"deleted"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type SyncEvent struct {
	ID        int64     `json:"id"`
	UserID    string    `json:"-"`
	ItemID    string    `json:"item_id"`
	Path      string    `json:"path"`
	Type      string    `json:"type"`
	Version   int64     `json:"version"`
	Metadata  string    `json:"metadata"`
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"created_at"`
}

type SyncSession struct {
	SessionID     string    `json:"session_id"`
	UserID        string    `json:"-"`
	DeviceID      string    `json:"device_id"`
	CursorVersion int64     `json:"cursor"`
	CreatedAt     time.Time `json:"created_at"`
	LastSeenAt    time.Time `json:"last_seen_at"`
}
