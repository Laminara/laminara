package control

import (
	"context"
	"net"
	"net/http"
)

const baseURL = "http://laminara"

func BaseURL() string { return baseURL }

func HTTPClient() *http.Client {
	path := SocketPath()
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", path)
			},
		},
	}
}
