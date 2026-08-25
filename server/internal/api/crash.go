package api

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
	"github.com/laminara/laminara/server/internal/crash"
)

func (s *Service) ReportCrash(ctx context.Context, req *connect.Request[apiv1.ReportCrashRequest]) (*connect.Response[apiv1.ReportCrashResponse], error) {
	subject, state := s.subjectOf(ctx, req.Header())
	if state != tokenValid {
		return nil, connect.NewError(connect.CodeUnauthenticated, errStaleSession)
	}
	if s.crashes == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("сервер не принимает отчёты о падениях"))
	}
	incoming := req.Msg.Crash
	if incoming == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("отчёт пуст"))
	}

	log := incoming.Log
	if len(log) > crash.MaxLogBytes {
		log = log[len(log)-crash.MaxLogBytes:]
	}
	happened := time.Now()
	if incoming.HappenedAtUnixNanos > 0 {
		happened = time.Unix(0, incoming.HappenedAtUnixNanos)
	}

	report := crash.Report{
		Player:    subject.Username,
		UUID:      subject.UUID,
		Build:     incoming.Build,
		Version:   incoming.BuildVersion,
		Loader:    incoming.Loader,
		ExitCode:  incoming.ExitCode,
		Log:       log,
		Details:   incoming.Details,
		Happened:  happened,
		Launcher:  incoming.Details["launcher"],
		Platform:  incoming.Details["platform"],
		OSVersion: incoming.Details["os"],
	}

	if err := s.crashes.Accept(ctx, report, s.log); err != nil {
		return connect.NewResponse(&apiv1.ReportCrashResponse{Accepted: false, Message: err.Error()}), nil
	}
	return connect.NewResponse(&apiv1.ReportCrashResponse{
		Accepted: true,
		Message:  "Отчёт отправлен, спасибо",
	}), nil
}
