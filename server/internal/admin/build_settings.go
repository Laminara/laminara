package admin

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
)

type BuildSettings interface {
	PreviewBuildSettings(context.Context, string, *adminv1.PreviewSettingsRequest) (*adminv1.PreviewSettingsResponse, error)
	ApplyBuildSettings(context.Context, string, *adminv1.SettingsPatch) (string, error)
}

func (s *Service) SetBuildSettings(builds BuildSettings) { s.buildSettings = builds }

func (s *Service) PreviewBuildSettings(ctx context.Context, request *connect.Request[adminv1.PreviewBuildSettingsRequest]) (*connect.Response[adminv1.PreviewBuildSettingsResponse], error) {
	if s.buildSettings == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("сборки не настроены"))
	}
	view := request.Msg.Settings
	if view == nil {
		view = &adminv1.PreviewSettingsRequest{}
	}
	response, err := s.buildSettings.PreviewBuildSettings(ctx, request.Msg.Build, view)
	if err != nil {
		return nil, settingsPatchError(err)
	}
	return connect.NewResponse(&adminv1.PreviewBuildSettingsResponse{Settings: response}), nil
}

func (s *Service) ApplyBuildSettings(ctx context.Context, request *connect.Request[adminv1.ApplyBuildSettingsRequest]) (*connect.Response[adminv1.ApplyBuildSettingsResponse], error) {
	if s.buildSettings == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("сборки не настроены"))
	}
	revision, err := s.buildSettings.ApplyBuildSettings(ctx, request.Msg.Build, request.Msg.Patch)
	if err != nil {
		return nil, settingsPatchError(err)
	}
	return connect.NewResponse(&adminv1.ApplyBuildSettingsResponse{Revision: revision}), nil
}
