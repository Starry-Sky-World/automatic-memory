package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"deepseek2api-go/internal/auth"
	"deepseek2api-go/internal/config"
	"deepseek2api-go/internal/services"
	"deepseek2api-go/internal/state"
)

func ClaudeMessages(st *state.AppState) http.HandlerFunc {
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
		messagesAny, _ := req["messages"].([]any)
		if model == "" || len(messagesAny) == 0 {
			WriteJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"type": "invalid_request_error", "message": "Request must include 'model' and 'messages'."}})
			return
		}

		normalizedMessages := normalizeClaudeMessages(toMapSlice(messagesAny))
		toolsRequested := toMapSliceAny(req["tools"])
		payloadMessages := make([]map[string]any, 0, len(normalizedMessages)+2)

		if systemMsg := parseClaudeSystemMessage(req["system"]); systemMsg != nil {
			payloadMessages = append(payloadMessages, systemMsg)
		}
		payloadMessages = append(payloadMessages, normalizedMessages...)
		if len(toolsRequested) > 0 && !hasSystemRole(payloadMessages) {
			payloadMessages = append([]map[string]any{buildToolSystemMessage(toolsRequested)}, payloadMessages...)
		}

		deepseekModel := mapClaudeModel(cfg, model)
		thinkingEnabled, searchEnabled, _ := services.ResolveModelFlags(deepseekModel)
		finalPrompt := services.MessagesPrepare(payloadMessages)

		headers := auth.GetAuthHeaders(cfg, ac)
		sessionID, err := st.DeepSeek.CreateSession(r.Context(), headers, 3)
		if err != nil || sessionID == "" {
			if ac.UseConfigToken && auth.SwitchAccount(ac, st.Pool) {
				headers = auth.GetAuthHeaders(cfg, ac)
				sessionID, err = st.DeepSeek.CreateSession(r.Context(), headers, 3)
			}
		}
		if err != nil || sessionID == "" {
			WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"type": "invalid_request_error", "message": "invalid token."}})
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
			WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"type": "invalid_request_error", "message": "Failed to get PoW."}})
			return
		}

		headers["x-ds-pow-response"] = powResp
		payload := map[string]any{"chat_session_id": sessionID, "parent_message_id": nil, "client_stream_id": services.NewClientStreamID(), "prompt": finalPrompt, "ref_file_ids": []any{}, "thinking_enabled": thinkingEnabled, "search_enabled": searchEnabled}
		streaming, _ := req["stream"].(bool)
		if streaming {
			services.ClaudeStream(r.Context(), w, st.DeepSeek, headers, payload, model, normalizedMessages, toolsRequested)
			return
		}
		status, out := services.ClaudeNonStream(r.Context(), st.DeepSeek, headers, payload, model, normalizedMessages, toolsRequested)
		WriteJSON(w, status, out)
	}
}

func mapClaudeModel(cfg config.Config, model string) string {
	m := strings.ToLower(model)
	if strings.Contains(m, "opus") || strings.Contains(m, "reasoner") || strings.Contains(m, "slow") {
		if v := cfg.ClaudeModelMapping["slow"]; strings.TrimSpace(v) != "" {
			return v
		}
		return "deepseek-chat"
	}
	if v := cfg.ClaudeModelMapping["fast"]; strings.TrimSpace(v) != "" {
		return v
	}
	return "deepseek-chat"
}

func toMapSliceAny(v any) []map[string]any {
	arr, _ := v.([]any)
	out := make([]map[string]any, 0, len(arr))
	for _, it := range arr {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func normalizeClaudeMessages(messages []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		n := map[string]any{}
		for k, v := range m {
			n[k] = v
		}
		n["content"] = normalizeClaudeContent(m["content"])
		out = append(out, n)
	}
	return out
}

func normalizeClaudeContent(content any) any {
	arr, ok := content.([]any)
	if !ok {
		if s, ok := content.(string); ok {
			return strings.ToValidUTF8(s, "")
		}
		return content
	}
	parts := make([]string, 0, len(arr))
	for _, block := range arr {
		b, ok := block.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := b["type"].(string)
		switch typ {
		case "text":
			if t, ok := b["text"].(string); ok {
				parts = append(parts, strings.ToValidUTF8(t, ""))
			}
		case "tool_result":
			if c, ok := b["content"]; ok {
				parts = append(parts, fmt.Sprintf("%v", c))
			}
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	if len(arr) > 0 {
		return arr
	}
	return ""
}

func parseClaudeSystemMessage(v any) map[string]any {
	s := normalizeClaudeContent(v)
	if text, ok := s.(string); ok && strings.TrimSpace(text) != "" {
		return map[string]any{"role": "system", "content": text}
	}
	return nil
}

func hasSystemRole(messages []map[string]any) bool {
	for _, m := range messages {
		if r, _ := m["role"].(string); strings.EqualFold(r, "system") {
			return true
		}
	}
	return false
}

func buildToolSystemMessage(tools []map[string]any) map[string]any {
	infos := make([]string, 0, len(tools))
	for _, t := range tools {
		name, _ := t["name"].(string)
		desc, _ := t["description"].(string)
		if strings.TrimSpace(name) == "" {
			name = "unknown"
		}
		if strings.TrimSpace(desc) == "" {
			desc = "No description available"
		}
		infos = append(infos, "Tool: "+name+"\nDescription: "+desc)
	}
	content := "You are Claude, a helpful AI assistant. You have access to these tools:\n\n" + strings.Join(infos, "\n\n") + "\n\nWhen you need to use tools, output ONLY valid JSON in this format:\n{\"tool_calls\": [{\"name\": \"tool_name\", \"input\": {\"param\": \"value\"}}]}\n\nYou can call multiple tools in ONE response by including them in the same tool_calls array.\nDo not include any text outside the JSON structure."
	return map[string]any{"role": "system", "content": content}
}
