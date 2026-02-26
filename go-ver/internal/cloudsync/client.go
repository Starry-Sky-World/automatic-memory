package cloudsync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

var (
	ErrUnauthorized = fmt.Errorf("cloudsync unauthorized")
	ErrNotFound     = fmt.Errorf("cloudsync not found")
)

type ConflictError struct {
	ServerVersion int64
	ServerHash    string
}

func (e *ConflictError) Error() string { return "cloudsync conflict" }

type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
	userID     string
}

type HandshakeRequest struct {
	DeviceID string `json:"device_id"`
	Cursor   int64  `json:"cursor"`
}

type Session struct {
	SessionID string `json:"session_id"`
	DeviceID  string `json:"device_id"`
	Cursor    int64  `json:"cursor"`
}

type SyncItem struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Metadata any    `json:"metadata"`
	Version  int64  `json:"version"`
	Hash     string `json:"hash"`
	Deleted  bool   `json:"deleted"`
}

type SyncEvent struct {
	ID      int64  `json:"id"`
	ItemID  string `json:"item_id"`
	Path    string `json:"path"`
	Type    string `json:"type"`
	Version int64  `json:"version"`
}

type ListItemsResponse struct {
	Items         []SyncItem `json:"items"`
	NextCursor    int64      `json:"next_cursor"`
	LatestVersion int64      `json:"latest_version"`
}

type DeltaRequest struct {
	SinceVersion int64 `json:"since_version"`
	Limit        int   `json:"limit"`
	Cursor       int64 `json:"cursor"`
}

type DeltaResponse struct {
	Events     []SyncEvent `json:"events"`
	NextCursor int64       `json:"next_cursor"`
}

type UpsertRequest struct {
	Path        string `json:"path"`
	Metadata    any    `json:"metadata"`
	BaseVersion *int64 `json:"base_version,omitempty"`
}

type ResolveConflictRequest struct {
	Path        string `json:"path"`
	Metadata    any    `json:"metadata"`
	BaseVersion int64  `json:"base_version"`
}

type errorBody struct {
	Error         string `json:"error"`
	ServerVersion int64  `json:"server_version"`
	ServerHash    string `json:"server_hash"`
}

func NewClient(httpClient *http.Client, baseURL, token, userID string) *Client {
	return &Client{
		httpClient: httpClient,
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:      strings.TrimSpace(token),
		userID:     strings.TrimSpace(userID),
	}
}

func (c *Client) Handshake(ctx context.Context, req HandshakeRequest) (*Session, error) {
	var out Session
	if err := c.do(ctx, http.MethodPost, "/handshake", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListItems(ctx context.Context, sinceVersion int64, limit int, cursor int64) (*ListItemsResponse, error) {
	q := "?since_version=" + strconv.FormatInt(sinceVersion, 10) + "&limit=" + strconv.Itoa(limit) + "&cursor=" + strconv.FormatInt(cursor, 10)
	var out ListItemsResponse
	if err := c.do(ctx, http.MethodGet, "/items"+q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Delta(ctx context.Context, req DeltaRequest) (*DeltaResponse, error) {
	var out DeltaResponse
	if err := c.do(ctx, http.MethodPost, "/delta", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpsertItem(ctx context.Context, req UpsertRequest) (*SyncItem, error) {
	var out SyncItem
	if err := c.do(ctx, http.MethodPost, "/items", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ResolveConflict(ctx context.Context, req ResolveConflictRequest) (*SyncItem, error) {
	var out SyncItem
	if err := c.do(ctx, http.MethodPost, "/conflict/resolve", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		token := c.token
		if !strings.HasPrefix(strings.ToLower(token), "bearer ") {
			token = "Bearer " + token
		}
		req.Header.Set("Authorization", token)
	}
	if c.userID != "" {
		req.Header.Set("X-User-ID", c.userID)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil {
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}

	var eb errorBody
	_ = json.NewDecoder(resp.Body).Decode(&eb)
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		return &ConflictError{ServerVersion: eb.ServerVersion, ServerHash: eb.ServerHash}
	default:
		if strings.TrimSpace(eb.Error) != "" {
			return fmt.Errorf("cloudsync %d: %s", resp.StatusCode, eb.Error)
		}
		return fmt.Errorf("cloudsync status %d", resp.StatusCode)
	}
}
