package handlers

import (
	"encoding/json"
	"net/http"

	"deepseek2api-go/internal/auth"
	"deepseek2api-go/internal/services"
	"deepseek2api-go/internal/state"
)

func ClaudeTokens(st *state.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		cfg := st.GetConfig()
		ac, code, msg, err := auth.DetermineClaudeModeAndToken(r, cfg, st.Pool)
		if err != nil {
			WriteJSON(w, code, map[string]any{"error": map[string]any{"type": "invalid_request_error", "message": msg}})
			return
		}
		defer auth.ReleaseAccountIfNeeded(ac, st.Pool)
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"type": "invalid_request_error", "message": "Invalid JSON body."}})
			return
		}
		model, _ := req["model"].(string)
		messages, _ := req["messages"].([]any)
		if model == "" || len(messages) == 0 {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"type": "invalid_request_error", "message": "Request must include 'model' and 'messages'."}})
			return
		}
		count := len(services.MessagesPrepare(toMapSlice(messages))) / 4
		if count < 1 {
			count = 1
		}
		WriteJSON(w, http.StatusOK, map[string]any{"input_tokens": count})
	}
}

func toMapSlice(in []any) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, it := range in {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
