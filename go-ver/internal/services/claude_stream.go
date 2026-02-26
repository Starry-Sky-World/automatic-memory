package services

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"deepseek2api-go/internal/clients"
)

func ClaudeStream(ctx context.Context, w http.ResponseWriter, ds *clients.DeepSeekClient, headers map[string]string, payload map[string]any, model string, messages []map[string]any, toolsRequested []map[string]any) {
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
			errEvent := map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": "Stream processing error: " + err.Error()}}
			b, _ := json.Marshal(errEvent)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
			if flusher != nil {
				flusher.Flush()
			}
			return
		}

		finalText := ""
		finalThinking := ""
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
						finalThinking += seg.Text
					} else {
						finalText += seg.Text
					}
				}
				return !finished
			})
		}()

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
				errEvent := map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": "Invalid upstream stream."}}
				b, _ := json.Marshal(errEvent)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
				if flusher != nil {
					flusher.Flush()
				}
				return
			}
		}
		if finalText == "" && finalThinking == "" && attempt < maxRetries {
			time.Sleep(retryDelaySeconds * time.Duration(attempt+1))
			continue
		}

		messageID := fmt.Sprintf("msg_%d_%d", time.Now().Unix(), rand.Intn(9000)+1000)
		inputTokens := len(toJSON(messages)) / 4
		start := map[string]any{"type": "message_start", "message": map[string]any{"id": messageID, "type": "message", "role": "assistant", "model": model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": inputTokens, "output_tokens": 0}}}
		b, _ := json.Marshal(start)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
		detected := DetectToolCalls(finalText, toolsRequested)
		outputTokens := 0
		contentIndex := 0

		if finalThinking != "" {
			cbStart := map[string]any{"type": "content_block_start", "index": contentIndex, "content_block": map[string]any{"type": "thinking", "thinking": ""}}
			cbs, _ := json.Marshal(cbStart)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", string(cbs))
			cbDelta := map[string]any{"type": "content_block_delta", "index": contentIndex, "delta": map[string]any{"type": "thinking_delta", "thinking": finalThinking}}
			cbd, _ := json.Marshal(cbDelta)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", string(cbd))
			cbStop := map[string]any{"type": "content_block_stop", "index": contentIndex}
			cbe, _ := json.Marshal(cbStop)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", string(cbe))
			outputTokens += len(finalThinking) / 4
			contentIndex++
		}

		if len(detected) > 0 {
			for i, t := range detected {
				idx := contentIndex + i
				blk := map[string]any{"type": "content_block_start", "index": idx, "content_block": map[string]any{"type": "tool_use", "id": fmt.Sprintf("toolu_%d_%d_%d", time.Now().Unix(), rand.Intn(9000)+1000, idx), "name": t["name"], "input": t["input"]}}
				bb, _ := json.Marshal(blk)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", string(bb))
				stop := map[string]any{"type": "content_block_stop", "index": idx}
				bs, _ := json.Marshal(stop)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", string(bs))
				outputTokens += len(toJSON(t["input"])) / 4
			}
			delta := map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": outputTokens}}
			bd, _ := json.Marshal(delta)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", string(bd))
		} else {
			if finalText != "" {
				cbStart := map[string]any{"type": "content_block_start", "index": contentIndex, "content_block": map[string]any{"type": "text", "text": ""}}
				cbs, _ := json.Marshal(cbStart)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", string(cbs))
				cbDelta := map[string]any{"type": "content_block_delta", "index": contentIndex, "delta": map[string]any{"type": "text_delta", "text": finalText}}
				cbd, _ := json.Marshal(cbDelta)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", string(cbd))
				cbStop := map[string]any{"type": "content_block_stop", "index": contentIndex}
				cbe, _ := json.Marshal(cbStop)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", string(cbe))
				outputTokens += len(finalText) / 4
			}
			delta := map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": outputTokens}}
			bd, _ := json.Marshal(delta)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", string(bd))
		}
		stop := map[string]any{"type": "message_stop"}
		bs, _ := json.Marshal(stop)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(bs))
		if flusher != nil {
			flusher.Flush()
		}
		return
	}
}
