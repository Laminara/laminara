package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"connectrpc.com/connect"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
	"github.com/laminara/laminara/server/internal/hwid"
)

func (s *Service) GetChallenge(ctx context.Context, req *connect.Request[apiv1.GetChallengeRequest]) (*connect.Response[apiv1.GetChallengeResponse], error) {
	if !s.limits.ChallengeAllowed(ctx, clientIP(req.Header(), req.Peer().Addr)) {
		return nil, connect.NewError(connect.CodeResourceExhausted, errTooManyAttempts)
	}
	if !s.machines.Enabled() {
		return connect.NewResponse(&apiv1.GetChallengeResponse{ReportSchemaVersion: hwid.ReportSchemaVersion}), nil
	}
	nonce, expires, err := s.machines.Challenge()
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&apiv1.GetChallengeResponse{
		Nonce:               nonce,
		ExpiresUnixNanos:    expires.UnixNano(),
		ReportSchemaVersion: hwid.ReportSchemaVersion,
	}), nil
}

func (s *Service) ReportMachine(ctx context.Context, req *connect.Request[apiv1.ReportMachineRequest]) (*connect.Response[apiv1.ReportMachineResponse], error) {
	subject, state := s.subjectOf(ctx, req.Header())
	if state != tokenValid {
		return nil, connect.NewError(connect.CodeUnauthenticated, errStaleSession)
	}
	identity := hwid.Identity{Subject: subject.Subject, Username: subject.Username}
	verdict, err := s.machines.Check(ctx, identity, req.Msg.Machine, clientIP(req.Header(), req.Peer().Addr))
	if err != nil {
		return nil, machineError(err)
	}
	return connect.NewResponse(&apiv1.ReportMachineResponse{Verdict: verdict}), nil
}

var errTooManyAttempts = errors.New("too many attempts, wait a few minutes")

func machineError(err error) error {
	if banErr, ok := hwid.AsBanError(err); ok {
		connectErr := connect.NewError(connect.CodePermissionDenied, errors.New(banErr.Error()))
		if detail, detailErr := connect.NewErrorDetail(banErr.Ban.Info()); detailErr == nil {
			connectErr.AddDetail(detail)
		}
		return connectErr
	}
	switch {
	case errors.Is(err, hwid.ErrReportRequired),
		errors.Is(err, hwid.ErrReportInvalid),
		errors.Is(err, hwid.ErrChallengeStale),
		errors.Is(err, hwid.ErrVirtualMachine),
		errors.Is(err, hwid.ErrSoftwareKey):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	default:
		return err
	}
}

func clientIP(header http.Header, peer string) string {
	if forwarded := header.Get("X-Forwarded-For"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		if trimmed := strings.TrimSpace(first); trimmed != "" {
			return trimmed
		}
	}
	if real := strings.TrimSpace(header.Get("X-Real-IP")); real != "" {
		return real
	}
	if host, _, err := net.SplitHostPort(peer); err == nil {
		return host
	}
	return peer
}
