package admin

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
)

type recordingSettings struct {
	Settings
	method string
	value  string
}

func (s *recordingSettings) Set(path, value string) (*adminv1.SettingEntry, error) {
	s.method, s.value = "set", value
	return &adminv1.SettingEntry{Path: path}, nil
}

func (s *recordingSettings) Edit(path, value string) (*adminv1.SettingEntry, error) {
	s.method, s.value = "edit", value
	return &adminv1.SettingEntry{Path: path}, nil
}

func TestSettingWriteModesKeepReplacementSeparateFromRedactedEdits(t *testing.T) {
	for _, test := range []struct {
		mode   adminv1.SettingWriteMode
		value  string
		method string
	}{
		{adminv1.SettingWriteMode_SETTING_WRITE_MODE_UNSPECIFIED, "***", "set"},
		{adminv1.SettingWriteMode_SETTING_WRITE_MODE_REPLACE, "***", "set"},
		{adminv1.SettingWriteMode_SETTING_WRITE_MODE_EDIT_REDACTED, "***", "edit"},
		{adminv1.SettingWriteMode_SETTING_WRITE_MODE_CLEAR, "", "set"},
		{adminv1.SettingWriteMode_SETTING_WRITE_MODE_CLEAR, "unexpected", ""},
		{adminv1.SettingWriteMode(999), "***", ""},
	} {
		store := &recordingSettings{}
		service := &Service{settings: store}
		_, err := service.SetSetting(context.Background(), connect.NewRequest(&adminv1.SetSettingRequest{Path: "console.password", Value: test.value, Mode: test.mode}))
		if test.method == "" {
			if connect.CodeOf(err) != connect.CodeInvalidArgument || store.method != "" {
				t.Fatalf("invalid write reached storage: mode=%v err=%v", test.mode, err)
			}
			continue
		}
		if err != nil || store.method != test.method || store.value != test.value {
			t.Fatalf("mode=%v method=%s value=%q err=%v", test.mode, store.method, store.value, err)
		}
	}
}
