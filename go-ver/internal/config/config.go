package config

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

type AccountConfig struct {
	Email    string `json:"email"`
	Mobile   string `json:"mobile"`
	Password string `json:"password"`
	Token    string `json:"token"`
}

type CloudSyncConfig struct {
	Enabled         bool   `json:"enabled"`
	BaseURL         string `json:"base_url"`
	Token           string `json:"token"`
	UserID          string `json:"user_id"`
	DeviceID        string `json:"device_id"`
	IntervalSeconds int    `json:"interval_seconds"`
	Limit           int    `json:"limit"`
}

type Config struct {
	Keys               []string          `json:"keys"`
	Accounts           []AccountConfig   `json:"accounts"`
	Refresh            bool              `json:"refresh"`
	PowSolver          string            `json:"pow_solver"`
	MaxActiveAccounts  int               `json:"max_active_accounts"`
	ClaudeModelMapping map[string]string `json:"claude_model_mapping"`
	CloudSync          CloudSyncConfig   `json:"cloud_sync"`
	Port               string            `json:"-"`
	RequestTimeoutSec  int               `json:"-"`
	LogLevel           string            `json:"-"`
	DeepSeekHost       string            `json:"-"`
}

func Load() Config {
	cfg := Config{}
	if env := strings.TrimSpace(os.Getenv("API_CONFIG")); env != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(env), &m); err == nil {
			_ = json.Unmarshal([]byte(env), &cfg)
		}
	}
	if len(cfg.Keys) == 0 && len(cfg.Accounts) == 0 {
		paths := []string{os.Getenv("CONFIG_PATH"), "config.json", "../config.json"}
		for _, p := range paths {
			if strings.TrimSpace(p) == "" {
				continue
			}
			b, err := os.ReadFile(p)
			if err == nil {
				_ = json.Unmarshal(b, &cfg)
				break
			}
		}
	}
	if cfg.ClaudeModelMapping == nil {
		cfg.ClaudeModelMapping = map[string]string{"fast": "deepseek-chat", "slow": "deepseek-chat"}
	}
	cfg.Port = strings.TrimSpace(os.Getenv("PORT"))
	if cfg.Port == "" {
		cfg.Port = "5001"
	}
	cfg.LogLevel = strings.TrimSpace(os.Getenv("LOG_LEVEL"))
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	cfg.DeepSeekHost = strings.TrimSpace(os.Getenv("DEEPSEEK_HOST"))
	if cfg.DeepSeekHost == "" {
		cfg.DeepSeekHost = "chat.deepseek.com"
	}
	cfg.RequestTimeoutSec = 30
	if v := strings.TrimSpace(os.Getenv("REQUEST_TIMEOUT_SECONDS")); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			cfg.RequestTimeoutSec = i
		}
	}

	applyCloudSyncEnv(&cfg.CloudSync)
	if cfg.CloudSync.IntervalSeconds <= 0 {
		cfg.CloudSync.IntervalSeconds = 30
	}
	if cfg.CloudSync.Limit <= 0 {
		cfg.CloudSync.Limit = 100
	}
	if strings.TrimSpace(cfg.CloudSync.UserID) == "" {
		cfg.CloudSync.UserID = "default"
	}
	if strings.TrimSpace(cfg.CloudSync.DeviceID) == "" {
		if host, err := os.Hostname(); err == nil && strings.TrimSpace(host) != "" {
			cfg.CloudSync.DeviceID = "deepseek2api-" + host
		} else {
			cfg.CloudSync.DeviceID = "deepseek2api-device"
		}
	}
	cfg.CloudSync.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.CloudSync.BaseURL), "/")

	return cfg
}

func applyCloudSyncEnv(cs *CloudSyncConfig) {
	if v, ok := getenvBool("CLOUDSYNC_ENABLED"); ok {
		cs.Enabled = v
	}
	if v := strings.TrimSpace(os.Getenv("CLOUDSYNC_BASE_URL")); v != "" {
		cs.BaseURL = v
	}
	if v := strings.TrimSpace(os.Getenv("CLOUDSYNC_TOKEN")); v != "" {
		cs.Token = v
	}
	if v := strings.TrimSpace(os.Getenv("CLOUDSYNC_USER_ID")); v != "" {
		cs.UserID = v
	}
	if v := strings.TrimSpace(os.Getenv("CLOUDSYNC_DEVICE_ID")); v != "" {
		cs.DeviceID = v
	}
	if v := strings.TrimSpace(os.Getenv("CLOUDSYNC_INTERVAL_SECONDS")); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cs.IntervalSeconds = i
		}
	}
	if v := strings.TrimSpace(os.Getenv("CLOUDSYNC_LIMIT")); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			cs.Limit = i
		}
	}
}

func getenvBool(name string) (bool, bool) {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	if v == "" {
		return false, false
	}
	switch v {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func (c Config) URLLogin() string { return "https://" + c.DeepSeekHost + "/api/v0/users/login" }
func (c Config) URLSession() string {
	return "https://" + c.DeepSeekHost + "/api/v0/chat_session/create"
}
func (c Config) URLCreatePow() string {
	return "https://" + c.DeepSeekHost + "/api/v0/chat/create_pow_challenge"
}
func (c Config) URLCompletion() string {
	return "https://" + c.DeepSeekHost + "/api/v0/chat/completion"
}
func (c Config) BaseHeaders() map[string]string {
	return map[string]string{
		"Host":              c.DeepSeekHost,
		"User-Agent":        "DeepSeek/1.0.13 Android/35",
		"Accept":            "application/json",
		"Accept-Encoding":   "gzip",
		"Content-Type":      "application/json",
		"x-client-platform": "android",
		"x-client-version":  "1.3.0-auto-resume",
		"x-client-locale":   "zh_CN",
		"accept-charset":    "UTF-8",
	}
}
