package admin

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
)

type SettingsPage struct {
	Sections    []*adminv1.SettingSection
	Entries     []*adminv1.SettingEntry
	Collections []*adminv1.SettingCollection
	ConfigPath  string
	Pending     bool
}

type Settings interface {
	List(section, entry string) (SettingsPage, error)
	Set(path, value string) (*adminv1.SettingEntry, error)
	Add(collection, name string) (string, error)
	Remove(path string) error
	Restart() error
}

func (s *Service) settingsOrError() (Settings, error) {
	if s.settings == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("сервер запущен без файла настроек — править нечего"))
	}
	return s.settings, nil
}

func (s *Service) ListSettings(_ context.Context, req *connect.Request[adminv1.ListSettingsRequest]) (*connect.Response[adminv1.ListSettingsResponse], error) {
	store, err := s.settingsOrError()
	if err != nil {
		return nil, err
	}
	page, err := store.List(req.Msg.Section, req.Msg.Entry)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&adminv1.ListSettingsResponse{
		Sections:       page.Sections,
		Entries:        page.Entries,
		Collections:    page.Collections,
		ConfigPath:     page.ConfigPath,
		RestartPending: page.Pending,
	}), nil
}

func (s *Service) SetSetting(_ context.Context, req *connect.Request[adminv1.SetSettingRequest]) (*connect.Response[adminv1.SetSettingResponse], error) {
	store, err := s.settingsOrError()
	if err != nil {
		return nil, err
	}
	entry, err := store.Set(req.Msg.Path, req.Msg.Value)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&adminv1.SetSettingResponse{Entry: entry}), nil
}

func (s *Service) AddSettingEntry(_ context.Context, req *connect.Request[adminv1.AddSettingEntryRequest]) (*connect.Response[adminv1.AddSettingEntryResponse], error) {
	store, err := s.settingsOrError()
	if err != nil {
		return nil, err
	}
	path, err := store.Add(req.Msg.Collection, req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&adminv1.AddSettingEntryResponse{Path: path}), nil
}

func (s *Service) RemoveSettingEntry(_ context.Context, req *connect.Request[adminv1.RemoveSettingEntryRequest]) (*connect.Response[adminv1.RemoveSettingEntryResponse], error) {
	store, err := s.settingsOrError()
	if err != nil {
		return nil, err
	}
	if err := store.Remove(req.Msg.Path); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&adminv1.RemoveSettingEntryResponse{}), nil
}

func (s *Service) Restart(_ context.Context, _ *connect.Request[adminv1.RestartRequest]) (*connect.Response[adminv1.RestartResponse], error) {
	store, err := s.settingsOrError()
	if err != nil {
		return nil, err
	}
	if err := store.Restart(); err != nil {
		return nil, err
	}
	return connect.NewResponse(&adminv1.RestartResponse{}), nil
}
