package handlers

import (
	"net/http"

	"deepseek2api-go/internal/services"
)

func AnthropicModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	services.WriteAnthropicModels(w)
}
