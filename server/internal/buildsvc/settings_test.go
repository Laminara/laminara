package buildsvc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/settings"
)

func profileSetting(path, value string) *adminv1.SettingChange {
	return &adminv1.SettingChange{Action: &adminv1.SettingChange_Set{Set: &adminv1.SetSettingRequest{Path: path, Value: value}}}
}

func TestBuildSettingsClearShowsStoredSemantics(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pack")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := manifest.EnsureDefaultSettings(dir); err != nil {
		t.Fatal(err)
	}
	svc := NewService(nil, nil, root)
	doc, err := svc.settingsDoc("pack")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Set("profile.userWritable", ""); err != nil {
		t.Fatal(err)
	}
	fields, err := doc.Section("profile")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range fields {
		if field.Path == "profile.userWritable" && (field.IsSet || field.Value != "") {
			t.Fatalf("missing value shown as default: %+v", field)
		}
	}
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
	stored, err := manifest.LoadSettings(dir)
	if err != nil || len(stored.UserWritable) != 0 {
		t.Fatalf("stored policy: %+v, %v", stored.UserWritable, err)
	}
	group, err := doc.AddEntry("profile.features.groups", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Set(group+".id", "graphics"); err != nil {
		t.Fatal(err)
	}
	entries, err := doc.EntryFields(group)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range entries {
		if field.Path == group+".selection" && (!field.IsSet || field.Value != "multi") {
			t.Fatalf("new group selection: %+v", field)
		}
	}
	if err := doc.Set(group+".selection", ""); err != nil {
		t.Fatal(err)
	}
	entries, err = doc.EntryFields(group)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range entries {
		if field.Path == group+".selection" && (field.IsSet || field.Value != "") {
			t.Fatalf("cleared selection: %+v", field)
		}
	}
	if err := doc.Validate(); err == nil {
		t.Fatal("missing selection accepted")
	}
}

func TestBuildSettingsPreserveUnknownFieldsAndRejectConflicts(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "My Pack")
	if err := os.Mkdir(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(profile, manifest.SettingsFileName)
	before := `{"future":{"nested":true},"install":"recorded","serverAddress":"old"}`
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService(nil, nil, root)
	page, err := service.PreviewBuildSettings(context.Background(), "My Pack", &adminv1.PreviewSettingsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	patch := &adminv1.SettingsPatch{Revision: page.Page.Revision, Changes: []*adminv1.SettingChange{profileSetting("profile.serverAddress", "play.example.com:25565")}}
	preview, err := service.PreviewBuildSettings(context.Background(), "My Pack", &adminv1.PreviewSettingsRequest{Patch: patch})
	if err != nil || preview.ValidationError != "" {
		t.Fatalf("preview: %v %+v", err, preview)
	}
	data, _ := os.ReadFile(path)
	if string(data) != before {
		t.Fatal("preview saved the file")
	}
	if _, err := service.ApplyBuildSettings(context.Background(), "My Pack", patch); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	for _, want := range []string{"future", "nested", "recorded", "play.example.com:25565"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("lost %s", want)
		}
	}
	if _, err := service.ApplyBuildSettings(context.Background(), "My Pack", patch); !errors.Is(err, settings.ErrChanged) {
		t.Fatalf("stale patch: %v", err)
	}
	if err := manifest.SetLoader(profile, "fabric"); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "future") {
		t.Fatal("installer removed unknown settings")
	}
}

func TestBuildSettingsEditNestedOptionsWithoutJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "pack"), 0o700); err != nil {
		t.Fatal(err)
	}
	service := NewService(nil, nil, root)
	page, err := service.PreviewBuildSettings(context.Background(), "pack", &adminv1.PreviewSettingsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	add := func(path string) *adminv1.SettingChange {
		return &adminv1.SettingChange{Action: &adminv1.SettingChange_Add{Add: &adminv1.AddSettingEntryRequest{Collection: path}}}
	}
	patch := &adminv1.SettingsPatch{Revision: page.Page.Revision, Changes: []*adminv1.SettingChange{
		add("profile.features.groups"), profileSetting("profile.features.groups.0.id", "visuals"), profileSetting("profile.features.groups.0.title", "Оформление"),
		add("profile.features.groups.0.options"), profileSetting("profile.features.groups.0.options.0.id", "plain"), profileSetting("profile.features.groups.0.options.0.title", "Обычное"),
		add("profile.features.groups.0.options.0.groups"), profileSetting("profile.features.groups.0.options.0.groups.0.id", "details"),
		add("profile.features.groups.0.options.0.groups.0.options"), profileSetting("profile.features.groups.0.options.0.groups.0.options.0.id", "extra"),
	}}
	preview, err := service.PreviewBuildSettings(context.Background(), "pack", &adminv1.PreviewSettingsRequest{Patch: patch, Entry: "profile.features.groups.0.options.0"})
	if err != nil || preview.ValidationError != "" {
		t.Fatalf("nested preview: %v %+v", err, preview)
	}
	if len(preview.Page.Collections) != 1 || len(preview.Page.Collections[0].Entries) != 1 {
		t.Fatal("nested groups missing from UI")
	}
	if _, err := service.ApplyBuildSettings(context.Background(), "pack", patch); err != nil {
		t.Fatal(err)
	}
	value, err := manifest.LoadSettings(filepath.Join(root, "pack"))
	if err != nil || value.Features.Groups[0].Options[0].Groups[0].Options[0].ID != "extra" {
		t.Fatalf("nested settings not persisted: %v", err)
	}
}
