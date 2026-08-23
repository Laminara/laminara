package admin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"connectrpc.com/connect"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/logbus"
	"github.com/laminara/laminara/server/internal/progress"
)

type Status struct {
	Version       string
	StartedAtNano int64
	ModulesLoaded uint32
	MemoryBytes   uint64
	Goroutines    uint32
}

type StatusFunc func() Status

type Service struct {
	status   StatusFunc
	bus      *logbus.Bus
	registry *command.Registry
	catalog  Catalog
}

func NewService(status StatusFunc, bus *logbus.Bus, registry *command.Registry, catalog Catalog) *Service {
	return &Service{status: status, bus: bus, registry: registry, catalog: catalog}
}

func (s *Service) ListVersions(ctx context.Context, req *connect.Request[adminv1.ListVersionsRequest]) (*connect.Response[adminv1.ListVersionsResponse], error) {
	if s.catalog == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("build service not configured"))
	}
	list, err := s.catalog.Versions(ctx, req.Msg.Query)
	if err != nil {
		return nil, err
	}
	resp := &adminv1.ListVersionsResponse{LatestRelease: list.LatestRelease, LatestSnapshot: list.LatestSnapshot}
	for _, version := range list.Versions {
		resp.Versions = append(resp.Versions, &adminv1.VersionInfo{Id: version.ID, Type: version.Type})
	}
	return connect.NewResponse(resp), nil
}

func (s *Service) ListLoaders(ctx context.Context, req *connect.Request[adminv1.ListLoadersRequest]) (*connect.Response[adminv1.ListLoadersResponse], error) {
	if s.catalog == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("build service not configured"))
	}
	loaders, err := s.catalog.Loaders(ctx, req.Msg.McVersion)
	if err != nil {
		return nil, err
	}
	resp := &adminv1.ListLoadersResponse{}
	for _, entry := range loaders {
		resp.Loaders = append(resp.Loaders, &adminv1.LoaderInfo{Name: entry.Name, Versions: entry.Versions})
	}
	return connect.NewResponse(resp), nil
}

func (s *Service) ListBuilds(_ context.Context, _ *connect.Request[adminv1.ListBuildsRequest]) (*connect.Response[adminv1.ListBuildsResponse], error) {
	if s.catalog == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("build service not configured"))
	}
	builds, err := s.catalog.Builds()
	if err != nil {
		return nil, err
	}
	resp := &adminv1.ListBuildsResponse{}
	for _, entry := range builds {
		resp.Builds = append(resp.Builds, &adminv1.BuildInfo{Name: entry.Name, Status: entry.Status})
	}
	return connect.NewResponse(resp), nil
}

func (s *Service) Status(_ context.Context, _ *connect.Request[adminv1.StatusRequest]) (*connect.Response[adminv1.StatusResponse], error) {
	st := s.status()
	return connect.NewResponse(&adminv1.StatusResponse{
		Version:            st.Version,
		StartedAtUnixNanos: st.StartedAtNano,
		ModulesLoaded:      st.ModulesLoaded,
		MemoryBytes:        st.MemoryBytes,
		Goroutines:         st.Goroutines,
	}), nil
}

func (s *Service) StreamLogs(ctx context.Context, req *connect.Request[adminv1.StreamLogsRequest], stream *connect.ServerStream[adminv1.StreamLogsResponse]) error {
	minLevel := req.Msg.MinLevel
	source := req.Msg.Source
	history, live, cancel := s.bus.Subscribe(int(req.Msg.Backscroll))
	defer cancel()

	for _, l := range history {
		if !passes(l, minLevel, source) {
			continue
		}
		if err := stream.Send(&adminv1.StreamLogsResponse{Line: toProto(l)}); err != nil {
			return err
		}
	}
	if !req.Msg.Follow {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case l, ok := <-live:
			if !ok {
				return nil
			}
			if !passes(l, minLevel, source) {
				continue
			}
			if err := stream.Send(&adminv1.StreamLogsResponse{Line: toProto(l)}); err != nil {
				return err
			}
		}
	}
}

func (s *Service) Exec(ctx context.Context, req *connect.Request[adminv1.ExecRequest], stream *connect.ServerStream[adminv1.ExecResponse]) error {
	sender := &streamSender{stream: stream}
	ctx = progress.With(ctx, &streamReporter{sender: sender})
	out := &chunkWriter{sender: sender}
	code := int32(0)
	if err := s.registry.Dispatch(ctx, req.Msg.Line, out); err != nil {
		code = 1
		fmt.Fprintf(&chunkWriter{sender: sender, stderr: true}, "%v\n", err)
	}
	return sender.send(&adminv1.ExecResponse{
		Event: &adminv1.ExecResponse_Result{Result: &adminv1.ExecResult{ExitCode: code}},
	})
}

type streamSender struct {
	stream *connect.ServerStream[adminv1.ExecResponse]
	mu     sync.Mutex
}

func (s *streamSender) send(resp *adminv1.ExecResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stream.Send(resp)
}

type streamReporter struct {
	sender *streamSender
}

func (r *streamReporter) Report(event progress.Event) {
	_ = r.sender.send(&adminv1.ExecResponse{
		Event: &adminv1.ExecResponse_Progress{Progress: &adminv1.ExecProgress{
			Phase:   event.Phase,
			Current: event.Current,
			Total:   event.Total,
			Message: event.Message,
		}},
	})
}

type chunkWriter struct {
	sender *streamSender
	stderr bool
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	if err := w.sender.send(&adminv1.ExecResponse{
		Event: &adminv1.ExecResponse_Output{
			Output: &adminv1.ExecOutputChunk{Text: string(p), Stderr: w.stderr},
		},
	}); err != nil {
		return 0, err
	}
	return len(p), nil
}

func toProto(l logbus.Line) *adminv1.LogLine {
	return &adminv1.LogLine{
		TimeUnixNanos: l.Time.UnixNano(),
		Level:         levelToProto(l.Level),
		Source:        l.Source,
		Message:       l.Message,
		Fields:        l.Fields,
	}
}

func levelToProto(l slog.Level) adminv1.LogLevel {
	switch {
	case l <= slog.LevelDebug:
		return adminv1.LogLevel_LOG_LEVEL_DEBUG
	case l < slog.LevelWarn:
		return adminv1.LogLevel_LOG_LEVEL_INFO
	case l < slog.LevelError:
		return adminv1.LogLevel_LOG_LEVEL_WARN
	default:
		return adminv1.LogLevel_LOG_LEVEL_ERROR
	}
}

func passes(l logbus.Line, minLevel adminv1.LogLevel, source string) bool {
	if source != "" && l.Source != source {
		return false
	}
	if minLevel != adminv1.LogLevel_LOG_LEVEL_UNSPECIFIED && levelToProto(l.Level) < minLevel {
		return false
	}
	return true
}
