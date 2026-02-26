package httpserver

import (
	"net/http"
	"time"

	"deepseek2api-go/internal/handlers"
	"deepseek2api-go/internal/middleware"
	"deepseek2api-go/internal/state"
)

func NewRouter(st *state.AppState) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handlers.Root)
	mux.HandleFunc("/pool/status", handlers.PoolStatus(st))
	mux.HandleFunc("/sync/status", handlers.SyncStatus(st))
	mux.HandleFunc("/v1/models", handlers.OpenAIModels)
	mux.HandleFunc("/anthropic/v1/models", handlers.AnthropicModels)
	mux.HandleFunc("/v1/chat/completions", handlers.OpenAIChat(st))
	mux.HandleFunc("/anthropic/v1/messages", handlers.ClaudeMessages(st))
	mux.HandleFunc("/anthropic/v1/messages/count_tokens", handlers.ClaudeTokens(st))

	var h http.Handler = mux
	h = middleware.Recovery(h)
	h = middleware.Timeout(120 * time.Second)(h)
	h = middleware.CORS(h)
	return h
}
