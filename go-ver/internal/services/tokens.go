package services

import (
	"encoding/json"
	"net/http"
)

func WriteOpenAIModels(w http.ResponseWriter) {
	data := map[string]any{
		"object": "list",
		"data": []map[string]any{
			{"id": "deepseek-chat", "object": "model", "created": 1715635200, "owned_by": "deepseek"},
			{"id": "deepseek-v3", "object": "model", "created": 1715635200, "owned_by": "deepseek"},
			{"id": "deepseek-r1", "object": "model", "created": 1715635200, "owned_by": "deepseek"},
			{"id": "deepseek-reasoner", "object": "model", "created": 1715635200, "owned_by": "deepseek"},
			{"id": "deepseek-v3-search", "object": "model", "created": 1715635200, "owned_by": "deepseek"},
			{"id": "deepseek-chat-search", "object": "model", "created": 1715635200, "owned_by": "deepseek"},
			{"id": "deepseek-r1-search", "object": "model", "created": 1715635200, "owned_by": "deepseek"},
			{"id": "deepseek-reasoner-search", "object": "model", "created": 1715635200, "owned_by": "deepseek"},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(data)
}

func WriteAnthropicModels(w http.ResponseWriter) {
	data := map[string]any{
		"object": "list",
		"data": []map[string]any{
			{"id": "claude-sonnet-4-20250514", "object": "model", "created": 1715635200, "owned_by": "anthropic"},
			{"id": "claude-opus-4-20250514", "object": "model", "created": 1715635200, "owned_by": "anthropic"},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(data)
}
