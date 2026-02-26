package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"deepseek2api-go/internal/auth"
	"deepseek2api-go/internal/services"
	"deepseek2api-go/internal/state"
)

func OpenAIChat(st *state.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		cfg := st.GetConfig()
		ac, code, msg, err := auth.DetermineModeAndToken(r, cfg, st.Pool)
		if err != nil {
			WriteJSON(w, code, map[string]any{"error": msg})
			return
		}
		defer auth.ReleaseAccountIfNeeded(ac, st.Pool)
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid JSON body."})
			return
		}
		model, _ := req["model"].(string)
		messagesAny, _ := req["messages"].([]any)
		if model == "" || len(messagesAny) == 0 {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": "Request must include 'model' and 'messages'."})
			return
		}
		messages := toMapSlice(messagesAny)
		for i := range messages {
			if s, ok := messages[i]["content"].(string); ok {
				messages[i]["content"] = strings.ToValidUTF8(s, "")
			}
		}
		thinkingEnabled, searchEnabled, ok := services.ResolveModelFlags(model)
		if !ok {
			WriteJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "Model '" + model + "' is not available."})
			return
		}
		finalPrompt := services.MessagesPrepare(messages)
		headers := auth.GetAuthHeaders(cfg, ac)
		sessionID, err := st.DeepSeek.CreateSession(r.Context(), headers, 3)
		if err != nil || sessionID == "" {
			if ac.UseConfigToken && auth.SwitchAccount(ac, st.Pool) {
				headers = auth.GetAuthHeaders(cfg, ac)
				sessionID, err = st.DeepSeek.CreateSession(r.Context(), headers, 3)
			}
		}
		if err != nil || sessionID == "" {
			WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid token."})
			return
		}
		powResp, err := st.DeepSeek.GetPoW(r.Context(), headers, st.PowSolver, st.PowCache, 3)
		if err != nil || powResp == "" {
			if ac.UseConfigToken && auth.SwitchAccount(ac, st.Pool) {
				headers = auth.GetAuthHeaders(cfg, ac)
				powResp, err = st.DeepSeek.GetPoW(r.Context(), headers, st.PowSolver, st.PowCache, 3)
			}
		}
		if err != nil || powResp == "" {
			WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": "Failed to get PoW (invalid token or unknown error)."})
			return
		}
		headers["x-ds-pow-response"] = powResp
		payload := map[string]any{"chat_session_id": sessionID, "parent_message_id": nil, "client_stream_id": services.NewClientStreamID(), "prompt": finalPrompt, "ref_file_ids": []any{}, "thinking_enabled": thinkingEnabled, "search_enabled": searchEnabled}
		created := time.Now().Unix()
		completionID := sessionID
		streaming, _ := req["stream"].(bool)
		if streaming {
			services.OpenAIStream(r.Context(), w, st.DeepSeek, headers, payload, model, finalPrompt, completionID, created, thinkingEnabled, searchEnabled)
			return
		}
		status, out := services.OpenAINonStream(r.Context(), st.DeepSeek, headers, payload, model, finalPrompt, completionID, created, thinkingEnabled, searchEnabled)
		WriteJSON(w, status, out)
	}
}
