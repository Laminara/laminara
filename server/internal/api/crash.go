package api

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
	"github.com/laminara/laminara/server/internal/crash"
)

var errReportsOff = errors.New("сервер не принимает отчёты")

func (s *Service) ReportCrash(ctx context.Context, req *connect.Request[apiv1.ReportCrashRequest]) (*connect.Response[apiv1.ReportCrashResponse], error) {
	subject, state := s.subjectOf(ctx, req.Header())
	if state != tokenValid {
		return nil, connect.NewError(connect.CodeUnauthenticated, errStaleSession)
	}
	if s.crashes == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errReportsOff)
	}
	incoming := req.Msg.Crash
	if incoming == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("отчёт пуст"))
	}

	report := describedReport(crash.GameCrash, incoming.Log, incoming.Details, incoming.HappenedAtUnixNanos)
	report.Player = subject.Username
	report.UUID = subject.UUID
	report.Build = incoming.Build
	report.Version = incoming.BuildVersion
	report.Loader = incoming.Loader
	report.ExitCode = incoming.ExitCode

	if err := s.crashes.Accept(context.WithoutCancel(ctx), report, s.log); err != nil {
		return connect.NewResponse(&apiv1.ReportCrashResponse{Accepted: false, Message: err.Error()}), nil
	}
	return connect.NewResponse(&apiv1.ReportCrashResponse{Accepted: true, Message: "Отчёт отправлен, спасибо"}), nil
}

func (s *Service) ReportLauncherLog(ctx context.Context, req *connect.Request[apiv1.ReportLauncherLogRequest]) (*connect.Response[apiv1.ReportLauncherLogResponse], error) {
	if s.crashes == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errReportsOff)
	}
	incoming := req.Msg.Report
	if incoming == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("журнал пуст"))
	}

	report := describedReport(crash.LauncherLog, incoming.Log, incoming.Details, incoming.HappenedAtUnixNanos)
	var err error
	if subject, state := s.subjectOf(ctx, req.Header()); state == tokenValid {
		report.Player = subject.Username
		report.UUID = subject.UUID
		err = s.crashes.Accept(context.WithoutCancel(ctx), report, s.log)
	} else {
		address := s.proxies.Of(req.Header(), req.Peer().Addr)
		err = s.crashes.AcceptAnonymous(context.WithoutCancel(ctx), report, address, s.log)
	}
	if err != nil {
		return connect.NewResponse(&apiv1.ReportLauncherLogResponse{Accepted: false, Message: err.Error()}), nil
	}
	return connect.NewResponse(&apiv1.ReportLauncherLogResponse{Accepted: true, Message: "Журнал отправлен, спасибо"}), nil
}

func describedReport(kind crash.Kind, log string, details map[string]string, happenedNanos int64) crash.Report {
	if len(log) > crash.MaxLogBytes {
		log = log[len(log)-crash.MaxLogBytes:]
	}
	happened := time.Now()
	if happenedNanos > 0 {
		happened = time.Unix(0, happenedNanos)
	}
	details = boundedDetails(details)
	return crash.Report{
		Kind:      kind,
		Log:       log,
		Details:   details,
		Happened:  happened,
		Launcher:  details["launcher"],
		Platform:  details["platform"],
		OSVersion: details["os"],
	}
}

func boundedDetails(details map[string]string) map[string]string {
	if len(details) <= crash.MaxDetails {
		return details
	}
	trimmed := make(map[string]string, crash.MaxDetails)
	for key, value := range details {
		if len(trimmed) >= crash.MaxDetails {
			break
		}
		if len(value) > crash.MaxDetailBytes {
			value = value[:crash.MaxDetailBytes]
		}
		trimmed[key] = value
	}
	return trimmed
}
