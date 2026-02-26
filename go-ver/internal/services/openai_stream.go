package services

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"deepseek2api-go/internal/clients"
)

func OpenAIStream(ctx context.Context, w http.ResponseWriter, ds *clients.DeepSeekClient, headers map[string]string, payload map[string]any, model, finalPrompt, completionID string, created int64, thinkingEnabled bool, searchEnabled bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err := ds.CompletionRawStreamRequest(ctx, headers, payload)
		if err != nil {
			if attempt < maxRetries {
				time.Sleep(retryDelaySeconds * time.Duration(attempt+1))
				continue
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"error":"Upstream connection failed after retries"}`)
			_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		}

		finalText := ""
		finalThinking := ""
		firstChunk := false
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
					v := seg.Text
					if searchEnabled && strings.HasPrefix(v, "[citation:") {
						continue
					}
					delta := map[string]any{}
					if !firstChunk {
						delta["role"] = "assistant"
						firstChunk = true
					}
					if seg.Type == "thinking" {
						if thinkingEnabled {
							finalThinking += v
							delta["reasoning_content"] = v
						}
					} else {
						finalText += v
						delta["content"] = v
					}
					if len(delta) > 0 {
						out := map[string]any{"id": completionID, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []map[string]any{{"delta": delta, "index": 0}}}
						b, _ := json.Marshal(out)
						_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
						if flusher != nil {
							flusher.Flush()
						}
					}
				}
				if finished && !firstChunk && finalText == "" && finalThinking == "" && attempt < maxRetries {
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
					if thinkingEnabled {
						finalThinking = jThinking
					}
					if !firstChunk {
						delta := map[string]any{"role": "assistant"}
						if finalText != "" {
							delta["content"] = finalText
						}
						if finalThinking != "" {
							delta["reasoning_content"] = finalThinking
						}
						out := map[string]any{"id": completionID, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []map[string]any{{"delta": delta, "index": 0}}}
						b, _ := json.Marshal(out)
						_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
						if flusher != nil {
							flusher.Flush()
						}
						firstChunk = true
					}
				}
			}
			if !firstChunk && finalText == "" && finalThinking == "" {
				if attempt < maxRetries {
					time.Sleep(retryDelaySeconds * time.Duration(attempt+1))
					continue
				}
				_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"error":"Invalid upstream stream"}`)
				_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
				if flusher != nil {
					flusher.Flush()
				}
				return
			}
		}
		if !firstChunk && finalText == "" && finalThinking == "" && attempt < maxRetries {
			time.Sleep(retryDelaySeconds * time.Duration(attempt+1))
			continue
		}

		if !sawSSEData && !firstChunk {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"error":"Invalid upstream stream"}`)
			_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		}

		promptTokens := len(finalPrompt) / 4
		reasoningTokens := len(finalThinking) / 4
		completionTokens := len(finalText) / 4
		finish := map[string]any{"id": completionID, "object": "chat.completion.chunk", "created": created, "model": model, "choices": []map[string]any{{"delta": map[string]any{}, "index": 0, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": promptTokens, "completion_tokens": reasoningTokens + completionTokens, "total_tokens": promptTokens + reasoningTokens + completionTokens, "completion_tokens_details": map[string]any{"reasoning_tokens": reasoningTokens}}}
		b, _ := json.Marshal(finish)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		return
	}
}
