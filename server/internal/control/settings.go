package control

import (
	"context"

	"connectrpc.com/connect"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
)

func PreviewSettings(ctx context.Context, client adminv1connect.AdminServiceClient, build string, request *adminv1.PreviewSettingsRequest) (*adminv1.PreviewSettingsResponse, error) {
	if build == "" {
		response, err := client.PreviewSettings(ctx, connect.NewRequest(request))
		if err != nil {
			return nil, err
		}
		return response.Msg, nil
	}
	response, err := client.PreviewBuildSettings(ctx, connect.NewRequest(&adminv1.PreviewBuildSettingsRequest{Build: build, Settings: request}))
	if err != nil {
		return nil, err
	}
	return response.Msg.Settings, nil
}

func ApplySettings(ctx context.Context, client adminv1connect.AdminServiceClient, patch *adminv1.SettingsPatch) (string, error) {
	if patch.TargetBuild == "" {
		response, err := client.ApplySettings(ctx, connect.NewRequest(&adminv1.ApplySettingsRequest{Patch: patch}))
		if err != nil {
			return "", err
		}
		return response.Msg.Revision, nil
	}
	response, err := client.ApplyBuildSettings(ctx, connect.NewRequest(&adminv1.ApplyBuildSettingsRequest{Build: patch.TargetBuild, Patch: patch}))
	if err != nil {
		return "", err
	}
	return response.Msg.Revision, nil
}
