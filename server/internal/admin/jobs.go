package admin

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
)

type Jobs interface {
	Start(context.Context, *adminv1.StartJobRequest) (*adminv1.JobInfo, error)
	Get(context.Context, string) (*adminv1.JobInfo, error)
	List(context.Context) ([]*adminv1.JobInfo, error)
	Cancel(context.Context, string) (*adminv1.JobInfo, error)
	Stream(context.Context, string, uint64, func(*adminv1.StreamJobResponse) error) error
}

func (s *Service) SetJobs(jobs Jobs) { s.jobs = jobs }

func (s *Service) StartJob(ctx context.Context, req *connect.Request[adminv1.StartJobRequest]) (*connect.Response[adminv1.StartJobResponse], error) {
	if s.jobs == nil {
		return nil, jobsUnavailable()
	}
	job, err := s.jobs.Start(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&adminv1.StartJobResponse{Job: job}), nil
}

func (s *Service) GetJob(ctx context.Context, req *connect.Request[adminv1.GetJobRequest]) (*connect.Response[adminv1.GetJobResponse], error) {
	if s.jobs == nil {
		return nil, jobsUnavailable()
	}
	job, err := s.jobs.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&adminv1.GetJobResponse{Job: job}), nil
}

func (s *Service) ListJobs(ctx context.Context, _ *connect.Request[adminv1.ListJobsRequest]) (*connect.Response[adminv1.ListJobsResponse], error) {
	if s.jobs == nil {
		return nil, jobsUnavailable()
	}
	jobs, err := s.jobs.List(ctx)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&adminv1.ListJobsResponse{Jobs: jobs}), nil
}

func (s *Service) CancelJob(ctx context.Context, req *connect.Request[adminv1.CancelJobRequest]) (*connect.Response[adminv1.CancelJobResponse], error) {
	if s.jobs == nil {
		return nil, jobsUnavailable()
	}
	job, err := s.jobs.Cancel(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&adminv1.CancelJobResponse{Job: job}), nil
}

func (s *Service) StreamJob(ctx context.Context, req *connect.Request[adminv1.StreamJobRequest], stream *connect.ServerStream[adminv1.StreamJobResponse]) error {
	if s.jobs == nil {
		return jobsUnavailable()
	}
	return s.jobs.Stream(ctx, req.Msg.Id, req.Msg.AfterSequence, stream.Send)
}

func jobsUnavailable() error {
	return connect.NewError(connect.CodeUnimplemented, errors.New("сервер запущен без каталога заданий"))
}
