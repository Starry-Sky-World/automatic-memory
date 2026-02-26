package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"cloud-sync/internal/middleware"
	"cloud-sync/internal/repos"
	"cloud-sync/internal/services"
	"github.com/gin-gonic/gin"
)

type SyncHandler struct {
	svc *services.SyncService
}

func NewSyncHandler(svc *services.SyncService) *SyncHandler {
	return &SyncHandler{svc: svc}
}

type conflictBody struct {
	Error         string `json:"error"`
	ServerVersion int64  `json:"server_version"`
	ServerHash    string `json:"server_hash"`
}

func (h *SyncHandler) UpsertItem(c *gin.Context) {
	userID := middleware.UserIDFromContext(c)
	path := strings.TrimSpace(c.PostForm("path"))
	metadataRaw := strings.TrimSpace(c.PostForm("metadata"))
	baseVersion, hasBase := parseOptionalInt64(c.PostForm("base_version"))

	var content []byte
	if fh, err := c.FormFile("file"); err == nil {
		f, err := fh.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file"})
			return
		}
		defer f.Close()
		content, _ = io.ReadAll(f)
	}

	if path == "" && strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "application/json") {
		var body struct {
			Path        string          `json:"path"`
			Metadata    json.RawMessage `json:"metadata"`
			BaseVersion *int64          `json:"base_version"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
			return
		}
		path = strings.TrimSpace(body.Path)
		metadataRaw = string(body.Metadata)
		if body.BaseVersion != nil {
			baseVersion = *body.BaseVersion
			hasBase = true
		}
	}

	var base *int64
	if hasBase {
		base = &baseVersion
	}
	item, err := h.svc.Upsert(userID, services.UpsertInput{
		Path:        path,
		Metadata:    json.RawMessage(metadataRaw),
		BaseVersion: base,
		Content:     content,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *SyncHandler) ListItems(c *gin.Context) {
	userID := middleware.UserIDFromContext(c)
	sinceVersion := parseInt64Default(c.Query("since_version"), 0)
	limit := int(parseInt64Default(c.Query("limit"), 50))
	cursor := parseInt64Default(c.Query("cursor"), 0)
	items, nextCursor, latest, err := h.svc.ListItems(userID, services.ListItemsInput{
		SinceVersion: sinceVersion,
		Limit:        limit,
		Cursor:       cursor,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items":          items,
		"next_cursor":    nextCursor,
		"latest_version": latest,
	})
}

func (h *SyncHandler) GetItem(c *gin.Context) {
	userID := middleware.UserIDFromContext(c)
	item, err := h.svc.GetItem(userID, c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *SyncHandler) DeleteItem(c *gin.Context) {
	h.updateDeleteState(c, true)
}

func (h *SyncHandler) RestoreItem(c *gin.Context) {
	h.updateDeleteState(c, false)
}

func (h *SyncHandler) Delta(c *gin.Context) {
	userID := middleware.UserIDFromContext(c)
	var body services.DeltaInput
	_ = c.ShouldBindJSON(&body)
	if body.Limit == 0 {
		body.Limit = 100
	}
	events, nextCursor, err := h.svc.Delta(userID, body)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events, "next_cursor": nextCursor})
}

func (h *SyncHandler) Handshake(c *gin.Context) {
	userID := middleware.UserIDFromContext(c)
	var body services.HandshakeInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
		return
	}
	s, err := h.svc.Handshake(userID, body)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, s)
}

func (h *SyncHandler) ResolveConflict(c *gin.Context) {
	userID := middleware.UserIDFromContext(c)
	var body services.ResolveConflictInput
	if strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		body.ID = c.PostForm("id")
		body.Path = c.PostForm("path")
		body.BaseVersion = parseInt64Default(c.PostForm("base_version"), 0)
		body.Metadata = json.RawMessage(c.PostForm("metadata"))
		if fh, err := c.FormFile("file"); err == nil {
			f, _ := fh.Open()
			defer f.Close()
			body.Content, _ = io.ReadAll(f)
		}
	} else {
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
			return
		}
	}
	item, err := h.svc.ResolveConflict(userID, body)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *SyncHandler) updateDeleteState(c *gin.Context, deleted bool) {
	userID := middleware.UserIDFromContext(c)
	var body struct {
		BaseVersion *int64 `json:"base_version"`
	}
	_ = c.ShouldBindJSON(&body)
	var (
		item any
		err  error
	)
	if deleted {
		item, err = h.svc.Delete(userID, c.Param("id"), body.BaseVersion)
	} else {
		item, err = h.svc.Restore(userID, c.Param("id"), body.BaseVersion)
	}
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *SyncHandler) writeError(c *gin.Context, err error) {
	var conflict *services.ConflictError
	switch {
	case errors.As(err, &conflict):
		c.JSON(http.StatusConflict, conflictBody{Error: "conflict", ServerVersion: conflict.ServerVersion, ServerHash: conflict.ServerHash})
	case errors.Is(err, repos.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
}

func parseInt64Default(v string, fallback int64) int64 {
	if i, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
		return i
	}
	return fallback
}

func parseOptionalInt64(v string) (int64, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	i, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false
	}
	return i, true
}
