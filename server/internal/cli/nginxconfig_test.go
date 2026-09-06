package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/config"
)

func fsConfig(t *testing.T, root, prefix string, xaccel bool) *config.Config {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"root": root, "xaccelPrefix": prefix})
	if err != nil {
		t.Fatal(err)
	}
	return &config.Config{
		API:     &config.APIConfig{Addr: "0.0.0.0:8099", XAccel: xaccel},
		Storage: &config.StorageConfig{Backend: "fs", Config: raw},
	}
}

func TestNginxFileStorage(t *testing.T) {
	opts, err := nginxOptionsOf(fsConfig(t, "/var/lib/laminara/objects", "/internal-objects/", true), []string{"launcher.example.com"}, true)
	if err != nil {
		t.Fatal(err)
	}
	out := renderNginx(opts)
	for _, want := range []string{
		"server 127.0.0.1:8099;",
		"server_name launcher.example.com;",
		"ssl_certificate     /etc/letsencrypt/live/launcher.example.com/fullchain.pem;",
		"location /internal-objects/ {",
		"alias /var/lib/laminara/objects/;",
		"return 301 https://$host$request_uri;",
		"map $http_upgrade $connection_upgrade {",
		"proxy_set_header Upgrade $http_upgrade;",
		"proxy_set_header Connection $connection_upgrade;",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestNginxWithoutXAccel(t *testing.T) {
	opts, err := nginxOptionsOf(fsConfig(t, "/var/lib/laminara/objects", "", false), []string{"launcher.example.com"}, false)
	if err != nil {
		t.Fatal(err)
	}
	out := renderNginx(opts)
	if strings.Contains(out, "internal;") {
		t.Fatalf("no internal location without xAccel:\n%s", out)
	}
	if strings.Contains(out, "ssl_certificate") || strings.Contains(out, "listen 443") {
		t.Fatalf("--no-tls must stay on :80:\n%s", out)
	}
}

func TestNginxNeedsListener(t *testing.T) {
	if _, err := nginxOptionsOf(&config.Config{}, []string{"launcher.example.com"}, true); err == nil {
		t.Fatal("a config without api.addr must be refused")
	}
}
