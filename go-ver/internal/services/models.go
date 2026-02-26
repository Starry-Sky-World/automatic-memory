package services

import (
	"bufio"
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"time"
)

type segment struct {
	Type string
	Text string
}

var debugDS = os.Getenv("DEBUG_DS") == "1"

func NewClientStreamID() string {
	return time.Now().UTC().Format("20060102") + "-" + randomHex16()
}

func randomHex16() string {
	const letters = "0123456789abcdef"
	b := make([]byte, 16)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func ResolveModelFlags(model string) (bool, bool, bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	switch m {
	case "deepseek-v3", "deepseek-chat":
		return false, false, true
	case "deepseek-r1", "deepseek-reasoner":
		return true, false, true
	case "deepseek-v3-search", "deepseek-chat-search":
		return false, true, true
	case "deepseek-r1-search", "deepseek-reasoner-search":
		return true, true, true
	default:
		return false, false, false
	}
}

func MessagesPrepare(messages []map[string]any) string {
	processed := make([]map[string]string, 0, len(messages))
	for _, m := range messages {
		role, _ := m["role"].(string)
		text := extractText(m["content"])
		processed = append(processed, map[string]string{"role": role, "text": text})
	}
	if len(processed) == 0 {
		return ""
	}
	merged := []map[string]string{processed[0]}
	for i := 1; i < len(processed); i++ {
		if processed[i]["role"] == merged[len(merged)-1]["role"] {
			merged[len(merged)-1]["text"] += "\n\n" + processed[i]["text"]
		} else {
			merged = append(merged, processed[i])
		}
	}

	parts := make([]string, 0, len(merged))
	for i, m := range merged {
		role := m["role"]
		text := m["text"]
		if role == "assistant" {
			parts = append(parts, "<｜Assistant｜>"+text+"<｜end▁of▁sentence｜>")
		} else if role == "user" || role == "system" {
			if i > 0 {
				parts = append(parts, "<｜User｜>"+text)
			} else {
				parts = append(parts, text)
			}
		} else {
			parts = append(parts, text)
		}
	}
	finalPrompt := strings.Join(parts, "")
	re := regexp.MustCompile(`!\[(.*?)\]\((.*?)\)`)
	finalPrompt = re.ReplaceAllString(finalPrompt, "[$1]($2)")
	return finalPrompt
}

func extractText(v any) string {
	switch vv := v.(type) {
	case string:
		return vv
	case []any:
		arr := make([]string, 0, len(vv))
		for _, it := range vv {
			if mp, ok := it.(map[string]any); ok {
				typ, _ := mp["type"].(string)
				switch typ {
				case "text":
					if t, ok := mp["text"].(string); ok {
						arr = append(arr, t)
					}
				case "tool_result":
					if c, ok := mp["content"]; ok {
						b, _ := json.Marshal(c)
						arr = append(arr, string(b))
					}
				}
			}
		}
		return strings.Join(arr, "\n")
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func parseChunk(chunk map[string]any, currentType string) (string, []segment, bool) {
	if debugDS {
		b, _ := json.Marshal(chunk)
		log.Printf("[DEBUG_DS] chunk=%s", string(b))
	}
	if currentType == "" {
		currentType = "text"
	}
	if p, ok := chunk["p"].(string); ok {
		switch p {
		case "response/search_status", "response/status":
			return currentType, nil, false
		case "response/thinking_content":
			currentType = "thinking"
		case "response/content":
			currentType = "text"
		}
	}

	segs := make([]segment, 0)
	finished := false
	switch vv := chunk["v"].(type) {
	case string:
		segs = append(segs, segment{Type: currentType, Text: vv})
	case []any:
		segType := currentType
		for _, item := range vv {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if p, ok := m["p"].(string); ok {
				switch p {
				case "status":
					if sv, _ := m["v"].(string); sv == "FINISHED" {
						finished = true
					}
					continue
				case "response/search_status", "response/status":
					continue
				case "response/thinking_content", "thinking_content":
					segType = "thinking"
				case "response/content", "content":
					segType = "text"
				}
			}
			if sv, ok := m["v"].(string); ok {
				segs = append(segs, segment{Type: segType, Text: sv})
			}
		}
	}
	return currentType, segs, finished
}

func ReadSSELines(scanner *bufio.Scanner, onData func(string) bool) {
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		if !onData(data) {
			break
		}
	}
}
