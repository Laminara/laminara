package serversetup_test

import (
	"slices"
	"testing"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/serversetup"
)

func standConfig() *config.Config {
	return &config.Config{
		Yggdrasil: &config.YggdrasilConfig{Enabled: true, SkinDomains: []string{"skins.example.com"}},
		Launcher:  &config.LauncherConfig{Endpoints: []string{"https://play.example.com/"}},
	}
}

func TestMirrorBaseGrowsFromTheLauncherEndpoint(t *testing.T) {
	base := serversetup.TextureMirrorBase(standConfig())
	if base != "https://play.example.com/yggdrasil/textures" {
		t.Fatalf("зеркало обязано жить на том же адресе, что и лаунчер: %s", base)
	}
}

func TestMirrorNeedsAnAddressPlayersCanReach(t *testing.T) {
	cfg := standConfig()
	cfg.Launcher.Endpoints = nil
	if base := serversetup.TextureMirrorBase(cfg); base != "" {
		t.Fatalf("без адреса для игроков зеркалить некуда: %s", base)
	}
}

func TestMirrorTurnsOffBySetting(t *testing.T) {
	cfg := standConfig()
	off := false
	cfg.Yggdrasil.MirrorTextures = &off
	if base := serversetup.TextureMirrorBase(cfg); base != "" {
		t.Fatalf("mirrorTextures=false обязан выключать зеркало: %s", base)
	}
	if domains := serversetup.TextureDomains(cfg); !slices.Equal(domains, []string{"skins.example.com"}) {
		t.Fatalf("без зеркала список доменов остаётся как в настройках: %v", domains)
	}
}

func TestMirrorHostJoinsTheAdvertisedDomains(t *testing.T) {
	domains := serversetup.TextureDomains(standConfig())
	if !slices.Equal(domains, []string{"skins.example.com", "play.example.com"}) {
		t.Fatalf("игра обязана принимать текстуры с самого сервера: %v", domains)
	}
}
