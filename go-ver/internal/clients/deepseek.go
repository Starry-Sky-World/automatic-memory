package clients

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"deepseek2api-go/internal/pow"
)

type DeepSeekClient struct {
	httpClient  *http.Client
	urlSession  string
	urlPow      string
	urlComplete string
	debug       bool
}

func NewDeepSeekClient(httpClient *http.Client, urlSession, urlPow, urlComplete string) *DeepSeekClient {
	return &DeepSeekClient{httpClient: httpClient, urlSession: urlSession, urlPow: urlPow, urlComplete: urlComplete, debug: os.Getenv("DEBUG_DS") == "1"}
}

func (c *DeepSeekClient) URLCompletion() string { return c.urlComplete }

func (c *DeepSeekClient) CreateSession(ctx context.Context, headers map[string]string, maxAttempts int) (string, error) {
	for i := 0; i < maxAttempts; i++ {
		payload := map[string]any{"agent": "chat"}
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.urlSession, bytes.NewReader(b))
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode == 200 {
			if code, ok := body["code"].(float64); ok && int(code) == 0 {
				if data, ok := body["data"].(map[string]any); ok {
					if biz, ok := data["biz_data"].(map[string]any); ok {
						if id, ok := biz["id"].(string); ok && id != "" {
							return id, nil
						}
					}
				}
			}
		}
		time.Sleep(time.Second)
	}
	return "", errors.New("failed create session")
}

func (c *DeepSeekClient) GetPoW(ctx context.Context, headers map[string]string, solver pow.Solver, cache *pow.Cache, maxAttempts int) (string, error) {
	for i := 0; i < maxAttempts; i++ {
		b, _ := json.Marshal(map[string]any{"target_path": "/api/v0/chat/completion"})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.urlPow, bytes.NewReader(b))
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode == 200 {
			if code, ok := body["code"].(float64); ok && int(code) == 0 {
				data, _ := body["data"].(map[string]any)
				biz, _ := data["biz_data"].(map[string]any)
				challenge, _ := biz["challenge"].(map[string]any)
				alg, _ := challenge["algorithm"].(string)
				chg, _ := challenge["challenge"].(string)
				salt, _ := challenge["salt"].(string)
				sig, _ := challenge["signature"].(string)
				targetPath, _ := challenge["target_path"].(string)
				difficulty := int(getFloat(challenge["difficulty"], 144000))
				expireAt := int64(getFloat(challenge["expire_at"], float64(time.Now().Unix()+60)))
				if c.debug {
					if chb, err := json.Marshal(challenge); err == nil {
						log.Printf("[DEBUG_DS] pow challenge raw=%s", string(chb))
					}
					log.Printf("[DEBUG_DS] pow challenge alg=%q difficulty=%d expire_at=%d target_path=%q", alg, difficulty, expireAt, targetPath)
				}
				key := pow.HashKey(alg, chg, salt, sig, targetPath)
				if v, ok := cache.Get(key); ok {
					return v, nil
				}
				ans, ok := solver.Solve(alg, chg, salt, difficulty, expireAt, sig, targetPath)
				if !ok {
					time.Sleep(time.Second)
					continue
				}
				pd := struct {
					Algorithm  string `json:"algorithm"`
					Challenge  string `json:"challenge"`
					Salt       string `json:"salt"`
					Answer     int64  `json:"answer"`
					Signature  string `json:"signature"`
					TargetPath string `json:"target_path"`
				}{
					Algorithm:  alg,
					Challenge:  chg,
					Salt:       salt,
					Answer:     ans,
					Signature:  sig,
					TargetPath: targetPath,
				}
				pb, _ := json.Marshal(pd)
				enc := base64.StdEncoding.EncodeToString(pb)
				if c.debug {
					log.Printf("[DEBUG_DS] pow response payload=%s", string(pb))
				}
				cache.Set(key, enc, expireAt)
				return enc, nil
			}
		}
		time.Sleep(time.Second)
	}
	return "", errors.New("failed get pow")
}

func (c *DeepSeekClient) CompletionStreamRequest(ctx context.Context, headers map[string]string, payload map[string]any) (*http.Response, error) {
	streamPayload := map[string]any{}
	for k, v := range payload {
		streamPayload[k] = v
	}
	streamPayload["stream"] = true

	b, _ := json.Marshal(streamPayload)
	if c.debug {
		log.Printf("[DEBUG_DS] completion stream payload=%s", string(b))
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.urlComplete, bytes.NewReader(b))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Del("Accept-Encoding")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		c.logCompletionResponse("completion_stream_fail", resp)
		defer resp.Body.Close()
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("upstream status=%d body=%s", resp.StatusCode, string(preview))
	}
	if c.debug {
		log.Printf("[DEBUG_DS] completion_stream_ok status=%d content_type=%s", resp.StatusCode, strings.TrimSpace(resp.Header.Get("Content-Type")))
	}
	return resp, nil
}

func (c *DeepSeekClient) CompletionJSONRequest(ctx context.Context, headers map[string]string, payload map[string]any) (map[string]any, error) {
	streamPayload := map[string]any{}
	for k, v := range payload {
		streamPayload[k] = v
	}
	streamPayload["stream"] = false

	b, _ := json.Marshal(streamPayload)
	if c.debug {
		log.Printf("[DEBUG_DS] completion json payload=%s", string(b))
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.urlComplete, bytes.NewReader(b))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Del("Accept-Encoding")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		c.logCompletionResponse("completion_json_fail", resp)
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("upstream status=%d body=%s", resp.StatusCode, string(preview))
	}
	if c.debug {
		c.logCompletionResponse("completion_json_ok", resp)
		log.Printf("[DEBUG_DS] completion_json_ok status=%d content_type=%s", resp.StatusCode, strings.TrimSpace(resp.Header.Get("Content-Type")))
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body, nil
}

func (c *DeepSeekClient) CompletionRawStreamRequest(ctx context.Context, headers map[string]string, payload map[string]any) (*http.Response, error) {
	b, _ := json.Marshal(payload)
	if c.debug {
		log.Printf("[DEBUG_DS] completion raw payload=%s", string(b))
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.urlComplete, bytes.NewReader(b))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Del("Accept-Encoding")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		c.logCompletionResponse("completion_raw_fail", resp)
		defer resp.Body.Close()
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("upstream status=%d body=%s", resp.StatusCode, string(preview))
	}
	if c.debug {
		c.logCompletionResponse("completion_raw_ok", resp)
		log.Printf("[DEBUG_DS] completion_raw_ok status=%d content_type=%s", resp.StatusCode, strings.TrimSpace(resp.Header.Get("Content-Type")))
	}
	return resp, nil
}

func (c *DeepSeekClient) logCompletionResponse(tag string, resp *http.Response) {
	if !c.debug || resp == nil {
		return
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "<empty>"
	}
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	preview := string(bodyBytes)
	log.Printf("[DEBUG_DS] %s status=%d content_type=%s body512=%q", tag, resp.StatusCode, contentType, preview)
	resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(bodyBytes), resp.Body))
}

func getFloat(v any, d float64) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return d
}
