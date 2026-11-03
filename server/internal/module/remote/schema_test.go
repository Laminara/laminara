package remote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdkmodule "github.com/laminara/laminara/sdk/go/module"
	"github.com/laminara/laminara/server/internal/settings"
)

type describedService struct {
	fakeService
	configured []byte
	reject     bool
	infoCalls  int
}

func (s *describedService) DescribeSettings(context.Context) (sdkmodule.Manifest, error) {
	return sdkmodule.Manifest{Info: sdkmodule.Info{Name: "Demo"}, SettingsSchemaJSON: []byte(`{"version":1,"module":[{"key":"token","kind":"secret","required":true},{"key":"enabled","kind":"bool","default":"true"}]}`)}, nil
}

func (s *describedService) Configure(_ context.Context, data []byte) error {
	s.configured = data
	if s.reject {
		return errors.New("SECRET in plugin error")
	}
	return nil
}

func (s *describedService) Info(ctx context.Context) (sdkmodule.Manifest, error) {
	s.infoCalls++
	return s.fakeService.Info(ctx)
}

func TestModuleDiscoverySurvivesMissingConfiguration(t *testing.T) {
	loader := NewLoader(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer loader.Close()
	svc := &describedService{}
	var hint string
	_, err := loader.configure(t.Context(), "/modules/discovery.test", nil, svc, func(_, value string) { hint = value })
	if err == nil || svc.infoCalls != 0 || svc.configured != nil {
		t.Fatal("module ran before required settings were available")
	}
	if !strings.Contains(hint, "Требует настройки") {
		t.Fatalf("status: %s", hint)
	}
	data, err := settings.PrepareModuleConfig("discovery.test", []byte(`{"token":"value"}`), true)
	if err != nil {
		t.Fatalf("schema was lost: %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config["enabled"] != true {
		t.Fatal("declared default missing")
	}
}

func TestConfigureErrorDoesNotExposePluginSecrets(t *testing.T) {
	loader := NewLoader(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer loader.Close()
	_, err := loader.configure(t.Context(), "/modules/rejected", []byte(`{"token":"value"}`), &describedService{reject: true}, func(string, string) {})
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestModuleConfigureReceivesEffectiveDefaults(t *testing.T) {
	loader := NewLoader(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer loader.Close()
	svc := &describedService{}
	var hint string
	_, err := loader.configure(t.Context(), "/modules/ready", []byte(`{"token":"value","unknown":"kept"}`), svc, func(_, value string) { hint = value })
	if err != nil {
		t.Fatal(err)
	}
	if svc.infoCalls != 1 || !strings.HasPrefix(hint, "Загружен.") {
		t.Fatalf("lifecycle: %d %s", svc.infoCalls, hint)
	}
	var values map[string]any
	if err := json.Unmarshal(svc.configured, &values); err != nil {
		t.Fatal(err)
	}
	if values["enabled"] != true || values["unknown"] != "kept" {
		t.Fatalf("configuration: %s", svc.configured)
	}
}

func TestSchemaRejectsTrailingDataAndUnknownVersion(t *testing.T) {
	for _, data := range []string{`{} {}`, `{"version":2}`, `{"providers":{"auth/missing":[]}}`} {
		t.Run(data, func(t *testing.T) {
			if _, err := decodeSchema(sdkmodule.Manifest{SettingsSchemaJSON: []byte(data)}); err == nil {
				t.Fatal("invalid schema accepted")
			}
		})
	}
}

type legacySettingsService struct {
	describedService
}

func (s *legacySettingsService) DescribeSettings(context.Context) (sdkmodule.Manifest, error) {
	return sdkmodule.Manifest{}, status.Error(codes.Unimplemented, "unknown method DescribeSettings")
}

func TestLegacyModuleWithoutDiscoveryStillConfigures(t *testing.T) {
	loader := NewLoader(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer loader.Close()
	svc := &legacySettingsService{}
	config := []byte(`{"legacy":true}`)
	_, err := loader.configure(t.Context(), "/modules/legacy", config, svc, func(string, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if string(svc.configured) != string(config) || svc.infoCalls != 1 {
		t.Fatalf("legacy lifecycle: config=%s info=%d", svc.configured, svc.infoCalls)
	}
}
