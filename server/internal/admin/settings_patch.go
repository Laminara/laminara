package admin

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/settings"
)

type SettingsPatcher interface {
	Preview(*adminv1.PreviewSettingsRequest) (*adminv1.PreviewSettingsResponse, error)
	Apply(*adminv1.SettingsPatch) (string, error)
}

func (s *Service) PreviewSettings(_ context.Context, request *connect.Request[adminv1.PreviewSettingsRequest]) (*connect.Response[adminv1.PreviewSettingsResponse], error) {
	patcher, ok := s.settings.(SettingsPatcher)
	if !ok {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("редактирование настроек недоступно"))
	}
	response, err := patcher.Preview(request.Msg)
	if err != nil {
		return nil, settingsPatchError(err)
	}
	return connect.NewResponse(response), nil
}

func (s *Service) ApplySettings(_ context.Context, request *connect.Request[adminv1.ApplySettingsRequest]) (*connect.Response[adminv1.ApplySettingsResponse], error) {
	patcher, ok := s.settings.(SettingsPatcher)
	if !ok {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("редактирование настроек недоступно"))
	}
	revision, err := patcher.Apply(request.Msg.Patch)
	if err != nil {
		return nil, settingsPatchError(err)
	}
	return connect.NewResponse(&adminv1.ApplySettingsResponse{Revision: revision}), nil
}

func settingsPatchError(err error) error {
	code := connect.CodeInvalidArgument
	if errors.Is(err, settings.ErrChanged) {
		code = connect.CodeAborted
	}
	return connect.NewError(code, err)
}
