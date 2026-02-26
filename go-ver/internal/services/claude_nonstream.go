package services

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"deepseek2api-go/internal/clients"
)

func ClaudeNonStream(ctx context.Context, ds *clients.DeepSeekClient, headers map[string]string, payload map[string]any, model string, normalizedMessages []map[string]any, toolsRequested []map[string]any) (int, map[string]any) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err := ds.CompletionRawStreamRequest(ctx, headers, payload)
		if err != nil {
			if attempt < maxRetries {
				time.Sleep(retryDelaySeconds * time.Duration(attempt+1))
				continue
			}
			return http.StatusBadGateway, map[string]any{"error": map[string]any{"type": "api_error", "message": "Upstream DeepSeek completion failed."}}
		}

		finalContent := ""
		finalReasoning := ""
		sawSSEData := false

		func() {
			defer resp.Body.Close()
			scanner := bufio.NewScanner(resp.Body)
			scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
			ptype := "text"
			finished := false
			ReadSSELines(scanner, func(data string) bool {
				sawSSEData = true
				var chunk map[string]any
				if json.Unmarshal([]byte(data), &chunk) != nil {
					return true
				}
				var segs []segment
				ptype, segs, finished = parseChunk(chunk, ptype)
				for _, seg := range segs {
					if seg.Type == "thinking" {
						finalReasoning += seg.Text
					} else {
						finalContent += seg.Text
					}
				}
				return !finished
			})
		}()

		if !sawSSEData {
			if body, jerr := ds.CompletionJSONRequest(ctx, headers, payload); jerr == nil {
				jThinking, jText, ok := extractCompletionFromJSON(body)
				if ok {
					finalContent = jText
					finalReasoning = jThinking
				}
			}
			if finalContent == "" && finalReasoning == "" {
				if attempt < maxRetries {
					time.Sleep(retryDelaySeconds * time.Duration(attempt+1))
					continue
				}
				return http.StatusBadGateway, map[string]any{"error": map[string]any{"type": "api_error", "message": "Invalid upstream stream."}}
			}
		}
		if finalContent == "" && finalReasoning == "" && attempt < maxRetries {
			time.Sleep(retryDelaySeconds * time.Duration(attempt+1))
			continue
		}

		detected := DetectToolCalls(finalContent, toolsRequested)
		out := map[string]any{
			"id":            "msg_" + strconvI64(time.Now().Unix()) + "_" + strconvI(rand.Intn(9000)+1000),
			"type":          "message",
			"role":          "assistant",
			"model":         model,
			"content":       []map[string]any{},
			"stop_reason":   ternary(len(detected) > 0, "tool_use", "end_turn"),
			"stop_sequence": nil,
			"usage":         map[string]any{"input_tokens": len(toJSON(normalizedMessages)) / 4, "output_tokens": (len(finalContent) + len(finalReasoning)) / 4},
		}
		content := out["content"].([]map[string]any)
		if finalReasoning != "" {
			content = append(content, map[string]any{"type": "thinking", "thinking": finalReasoning})
		}
		if len(detected) > 0 {
			for i, t := range detected {
				content = append(content, map[string]any{"type": "tool_use", "id": "toolu_" + strconvI(i+1) + "_" + strconvI(rand.Intn(9000)+1000), "name": t["name"], "input": t["input"]})
			}
		} else {
			if finalContent != "" || finalReasoning == "" {
				content = append(content, map[string]any{"type": "text", "text": firstNonEmpty(finalContent, "抱歉，没有生成有效的响应内容。")})
			}
		}
		out["content"] = content
		return http.StatusOK, out
	}
	return http.StatusBadGateway, map[string]any{"error": map[string]any{"type": "api_error", "message": "Upstream DeepSeek completion failed."}}
}

func DetectToolCalls(text string, tools []map[string]any) []map[string]any {
	clean := strings.TrimSpace(text)
	if !strings.HasPrefix(clean, "{\"tool_calls\":") || !strings.HasSuffix(clean, "]}") {
		return nil
	}
	var body map[string]any
	if json.Unmarshal([]byte(clean), &body) != nil {
		return nil
	}
	arr, _ := body["tool_calls"].([]any)
	allowed := map[string]bool{}
	for _, t := range tools {
		if n, ok := t["name"].(string); ok {
			allowed[n] = true
		}
	}
	out := make([]map[string]any, 0)
	for _, it := range arr {
		m, _ := it.(map[string]any)
		n, _ := m["name"].(string)
		if !allowed[n] {
			continue
		}
		inp, _ := m["input"].(map[string]any)
		out = append(out, map[string]any{"name": n, "input": inp})
	}
	return out
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
func strconvI(v int) string     { return fmt.Sprintf("%d", v) }
func strconvI64(v int64) string { return fmt.Sprintf("%d", v) }
func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
