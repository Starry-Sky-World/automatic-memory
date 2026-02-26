package clients

import (
	"net"
	"net/http"
	"net/http/cookiejar"
	"time"

	"deepseek2api-go/internal/config"
)

func NewHTTPClient(cfg config.Config) *http.Client {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		ForceAttemptHTTP2:   true,
		DialContext: (&net.Dialer{
			Timeout: 10 * time.Second,
		}).DialContext,
	}
	jar, _ := cookiejar.New(nil)
	timeoutSec := cfg.RequestTimeoutSec
	if timeoutSec < 120 {
		timeoutSec = 120
	}
	return &http.Client{Transport: transport, Timeout: time.Duration(timeoutSec) * time.Second, Jar: jar}
}
