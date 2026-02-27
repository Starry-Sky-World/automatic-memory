package http

import (
	"cloud-sync/internal/config"
	"cloud-sync/internal/handlers"
	"cloud-sync/internal/middleware"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func NewRouter(cfg config.Config, h *handlers.SyncHandler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestLogger())
	r.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "OPTIONS"},
		AllowHeaders: []string{"Authorization", "Content-Type", "X-User-ID"},
	}))

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	v1 := r.Group("/api/cloud-sync/v1")
	v1.Use(middleware.Auth(cfg))
	{
		v1.POST("/items", h.UpsertItem)
		v1.GET("/items", h.ListItems)
		v1.GET("/items/:id", h.GetItem)
		v1.POST("/items/:id/delete", h.DeleteItem)
		v1.POST("/items/:id/restore", h.RestoreItem)
		v1.POST("/delta", h.Delta)
		v1.POST("/handshake", h.Handshake)
		v1.POST("/conflict/resolve", h.ResolveConflict)
	}
	return r
}
