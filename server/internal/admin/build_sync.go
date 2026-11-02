package admin

import (
	"connectrpc.com/connect"
	"context"
	"errors"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
)

type BuildSync interface {
	PlanBuild(context.Context, []byte) (*adminv1.PlanBuildResponse, error)
	UploadBuildChunk(context.Context, *adminv1.UploadBuildChunkRequest) (*adminv1.UploadBuildChunkResponse, error)
}

func (s *Service) PlanBuild(ctx context.Context, request *connect.Request[adminv1.PlanBuildRequest]) (*connect.Response[adminv1.PlanBuildResponse], error) {
	sync, ok := s.buildSettings.(BuildSync)
	if !ok {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("сборки не настроены"))
	}
	result, err := sync.PlanBuild(ctx, request.Msg.SpecificationJson)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}

func (s *Service) UploadBuildChunk(ctx context.Context, request *connect.Request[adminv1.UploadBuildChunkRequest]) (*connect.Response[adminv1.UploadBuildChunkResponse], error) {
	sync, ok := s.buildSettings.(BuildSync)
	if !ok {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("сборки не настроены"))
	}
	result, err := sync.UploadBuildChunk(ctx, request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(result), nil
}
