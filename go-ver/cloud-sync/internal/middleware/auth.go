package middleware

import (
	"net/http"
	"strings"

	"cloud-sync/internal/config"
	"github.com/gin-gonic/gin"
)

const userIDKey = "userID"

func UserIDFromContext(c *gin.Context) string {
	if v, ok := c.Get(userIDKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func Auth(cfg config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := strings.TrimSpace(cfg.AuthToken)
		enforceExplicitUser := token != ""
		if token != "" {
			h := strings.TrimSpace(c.GetHeader("Authorization"))
			if !strings.HasPrefix(strings.ToLower(h), "bearer ") || strings.TrimSpace(h[7:]) != token {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				return
			}
		}

		userID := strings.TrimSpace(c.GetHeader("X-User-ID"))
		if userID == "" {
			if enforceExplicitUser {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "x-user-id required"})
				return
			}
			userID = "default"
		}
		c.Set(userIDKey, userID)
		c.Next()
	}
}
