package httpx

import (
	"net/http"
	"time"

	"github.com/laminara/laminara/server/internal/version"
)

func UserAgent() string {
	return "laminara-server/" + version.Current + " (+https://laminara.dev)"
}

func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: WithUserAgent(nil)}
}

func WithUserAgent(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &agentTransport{base: base}
}

type agentTransport struct {
	base http.RoundTripper
}

func (a *agentTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Header.Get("User-Agent") != "" {
		return a.base.RoundTrip(request)
	}
	cloned := request.Clone(request.Context())
	cloned.Header.Set("User-Agent", UserAgent())
	return a.base.RoundTrip(cloned)
}
