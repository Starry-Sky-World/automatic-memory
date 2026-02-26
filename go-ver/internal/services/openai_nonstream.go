package services

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"deepseek2api-go/internal/clients"
)

const (
	maxRetries        = 5
	retryDelaySeconds = 800 * time.Millisecond
)

func extractCompletionFromJSON(body map[string]any) (string, string, bool) {
	if code, ok := body["code"].(float64); ok && int(code) != 0 {
		return "", "", false
	}
	if choices, ok := body["choices"].([]any); ok && len(choices) > 0 {
		first, _ := choices[0].(map[string]any)
		msg, _ := first["message"].(map[string]any)
		content, _ := msg["content"].(string)
		reasoning, _ := msg["reasoning_content"].(string)
		return reasoning, content, true
	}
	data, _ := body["data"].(map[string]any)
	biz, _ := data["biz_data"].(map[string]any)
	choices, _ := biz["choices"].([]any)
	if len(choices) == 0 {
		return "", "", false
	}
	first, _ := choices[0].(map[string]any)
	msg, _ := first["message"].(map[string]any)
	if msg == nil {
		return "", "", false
	}
	content, _ := msg["content"].(string)
	reasoning, _ := msg["reasoning_content"].(string)
	return reasoning, content, true
}

func OpenAINonStream(ctx context.Context, ds *clients.DeepSeekClient, headers map[string]string, payload map[string]any, model, finalPrompt, completionID string, created int64, thinkingEnabled bool, searchEnabled bool) (int, map[string]any) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		finalText := ""
		finalThinking := ""

		resp, err := ds.CompletionRawStreamRequest(ctx, headers, payload)
		if err != nil {
			if body, jerr := ds.CompletionJSONRequest(ctx, headers, payload); jerr == nil {
				jThinking, jText, ok := extractCompletionFromJSON(body)
				if ok {
					finalText = jText
					finalThinking = jThinking
				}
			}
			if finalText != "" || finalThinking != "" {
				promptTokens := len(finalPrompt) / 4
				reasoningTokens := len(finalThinking) / 4
				completionTokens := len(finalText) / 4
				result := map[string]any{
					"id":      completionID,
					"object":  "chat.completion",
					"created": created,
					"model":   model,
					"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": finalText, "reasoning_content": finalThinking}, "finish_reason": "stop"}},
					"usage":   map[string]any{"prompt_tokens": promptTokens, "completion_tokens": reasoningTokens + completionTokens, "total_tokens": promptTokens + reasoningTokens + completionTokens, "completion_tokens_details": map[string]any{"reasoning_tokens": reasoningTokens}},
				}
				return http.StatusOK, result
			}
			if attempt < maxRetries {
				time.Sleep(retryDelaySeconds * time.Duration(attempt+1))
				continue
			}
			return http.StatusBadGateway, map[string]any{"error": "Upstream DeepSeek completion failed after retries."}
		}

		sawSSEData := false
		retryNow := false
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
					s := seg.Text
					if searchEnabled && strings.HasPrefix(s, "[citation:") {
						continue
					}
					if seg.Type == "thinking" {
						if thinkingEnabled {
							finalThinking += s
						}
					} else {
						finalText += s
					}
				}
				if finished && finalText == "" && finalThinking == "" && attempt < maxRetries {
					retryNow = true
					return false
				}
				return !finished
			})
		}()

		if retryNow {
			time.Sleep(retryDelaySeconds * time.Duration(attempt+1))
			continue
		}
		if !sawSSEData {
			if body, jerr := ds.CompletionJSONRequest(ctx, headers, payload); jerr == nil {
				jThinking, jText, ok := extractCompletionFromJSON(body)
				if ok {
					finalText = jText
					finalThinking = jThinking
				}
			}
			if finalText == "" && finalThinking == "" {
				if attempt < maxRetries {
					time.Sleep(retryDelaySeconds * time.Duration(attempt+1))
					continue
				}
				return http.StatusBadGateway, map[string]any{"error": "Upstream DeepSeek returned an invalid completion stream."}
			}
		}
		if finalText == "" && finalThinking == "" && attempt < maxRetries {
			time.Sleep(retryDelaySeconds * time.Duration(attempt+1))
			continue
		}

		promptTokens := len(finalPrompt) / 4
		reasoningTokens := len(finalThinking) / 4
		completionTokens := len(finalText) / 4
		result := map[string]any{
			"id":      completionID,
			"object":  "chat.completion",
			"created": created,
			"model":   model,
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": finalText, "reasoning_content": finalThinking}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": promptTokens, "completion_tokens": reasoningTokens + completionTokens, "total_tokens": promptTokens + reasoningTokens + completionTokens, "completion_tokens_details": map[string]any{"reasoning_tokens": reasoningTokens}},
		}
		return http.StatusOK, result
	}
	return http.StatusBadGateway, map[string]any{"error": "Upstream DeepSeek completion failed after retries."}
}
