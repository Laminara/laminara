package daemon

import (
	"errors"
	"strings"
	"testing"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/settings"
)

func settingChange(path, value string) *adminv1.SettingChange {
	return &adminv1.SettingChange{Action: &adminv1.SettingChange_Set{Set: &adminv1.SetSettingRequest{Path: path, Value: value, Mode: adminv1.SettingWriteMode_SETTING_WRITE_MODE_REPLACE}}}
}

func TestSettingsPreviewAndCommitAreSeparate(t *testing.T) {
	store := settingsStoreFor(t)
	before := readFile(t, store.path)
	page, err := store.List("", "")
	if err != nil {
		t.Fatal(err)
	}
	patch := &adminv1.SettingsPatch{Revision: page.Revision, Changes: []*adminv1.SettingChange{settingChange("auth.provider", "http"), settingChange("auth.config.url", "https://example.test/auth"), settingChange("auth.config.headers", `{"Authorization":"NEW-SECRET"}`)}}
	preview, err := store.Preview(&adminv1.PreviewSettingsRequest{Patch: patch, Section: "auth"})
	if err != nil {
		t.Fatal(err)
	}
	if readFile(t, store.path) != before || store.pending {
		t.Fatal("preview persisted settings")
	}
	for _, difference := range preview.Differences {
		if strings.Contains(difference.After, "NEW-SECRET") {
			t.Fatal("preview disclosed secret")
		}
	}
	revision, err := store.Apply(patch)
	if err != nil || revision == page.Revision {
		t.Fatalf("commit: %v", err)
	}
	if !strings.Contains(readFile(t, store.path), "NEW-SECRET") || !store.pending {
		t.Fatal("commit did not save all changes")
	}
	if _, err := store.Apply(patch); !errors.Is(err, settings.ErrChanged) {
		t.Fatalf("stale client overwrote settings: %v", err)
	}
}

func TestInvalidPatchNeverPartiallySaves(t *testing.T) {
	store := settingsStoreFor(t)
	page, _ := store.List("", "")
	before := readFile(t, store.path)
	patch := &adminv1.SettingsPatch{Revision: page.Revision, Changes: []*adminv1.SettingChange{settingChange("branding.name", "Mine"), settingChange("api.addr", "invalid-address")}}
	preview, err := store.Preview(&adminv1.PreviewSettingsRequest{Patch: patch})
	if err != nil || preview.ValidationError == "" {
		t.Fatalf("invalid patch needs a visible error: %v", err)
	}
	if _, err := store.Apply(patch); err == nil {
		t.Fatal("invalid patch accepted")
	}
	if readFile(t, store.path) != before {
		t.Fatal("invalid patch partially saved")
	}
}

func TestExternalConfigAllowsPreviewAndRefusesEveryWrite(t *testing.T) {
	store := settingsStoreFor(t)
	store.external = true
	page, _ := store.List("", "")
	patch := &adminv1.SettingsPatch{Revision: page.Revision, Changes: []*adminv1.SettingChange{settingChange("branding.name", "Mine")}}
	preview, err := store.Preview(&adminv1.PreviewSettingsRequest{Patch: patch})
	if err != nil || preview.Page.ReadOnlyReason == "" {
		t.Fatalf("external preview: %v", err)
	}
	if _, err := store.Apply(patch); !errors.Is(err, errExternalSettings) {
		t.Fatal(err)
	}
	if _, err := store.Set("branding.name", "Mine"); !errors.Is(err, errExternalSettings) {
		t.Fatal(err)
	}
	if _, err := store.Edit("branding.name", "Mine"); !errors.Is(err, errExternalSettings) {
		t.Fatal(err)
	}
	if _, err := store.Add("access.rules", ""); !errors.Is(err, errExternalSettings) {
		t.Fatal(err)
	}
	if err := store.Remove("access.rules.0"); !errors.Is(err, errExternalSettings) {
		t.Fatal(err)
	}
	if readFile(t, store.path) != "{}" {
		t.Fatal("external config changed")
	}
}
