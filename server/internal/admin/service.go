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
	status        StatusFunc
	bus           *logbus.Bus
	registry      *command.Registry
	catalog       Catalog
	settings      Settings
	buildSettings BuildSettings
	stop          func() error
	jobs          Jobs
}

func NewService(status StatusFunc, bus *logbus.Bus, registry *command.Registry, catalog Catalog) *Service {
	return &Service{status: status, bus: bus, registry: registry, catalog: catalog}
}

func (s *Service) SetSettings(store Settings) {
	s.settings = store
}

func (s *Service) SetStop(stop func() error) { s.stop = stop }

func (s *Service) Stop(context.Context, *connect.Request[adminv1.StopRequest]) (*connect.Response[adminv1.StopResponse], error) {
	if s.stop == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("остановка недоступна"))
	}
	if err := s.stop(); err != nil {
		return nil, err
	}
	return connect.NewResponse(&adminv1.StopResponse{}), nil
}

func (s *Service) ListCommands(context.Context, *connect.Request[adminv1.ListCommandsRequest]) (*connect.Response[adminv1.ListCommandsResponse], error) {
	response := &adminv1.ListCommandsResponse{}
	if s.registry != nil {
		for _, entry := range s.registry.List() {
			response.Commands = append(response.Commands, &adminv1.CommandInfo{Name: entry.Name, Aliases: entry.Aliases, Synopsis: entry.Synopsis, SecretArguments: entry.SecretArgs || entry.Secret, SecretOutput: entry.Secret})
		}
	}
	return connect.NewResponse(response), nil
}

func (s *Service) ListVersions(ctx context.Context, req *connect.Request[adminv1.ListVersionsRequest]) (*connect.Response[adminv1.ListVersionsResponse], error) {
	if s.catalog == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("сборки не настроены"))
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
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("сборки не настроены"))
	}
	loaders, err := s.catalog.Loaders(ctx, req.Msg.McVersion)
	if err != nil {
		return nil, err
	}
	resp := &adminv1.ListLoadersResponse{}
	for _, entry := range loaders {
		resp.Loaders = append(resp.Loaders, &adminv1.LoaderInfo{Name: entry.Name, Versions: entry.Versions, Trouble: entry.Trouble})
	}
	for _, entry := range s.catalog.Recipes(req.Msg.McVersion) {
		resp.Recipes = append(resp.Recipes, &adminv1.RecipeInfo{Name: entry.Name, Summary: entry.Summary, Loader: entry.Loader})
	}
	return connect.NewResponse(resp), nil
}

func (s *Service) ListBuilds(_ context.Context, _ *connect.Request[adminv1.ListBuildsRequest]) (*connect.Response[adminv1.ListBuildsResponse], error) {
	if s.catalog == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("сборки не настроены"))
	}
	builds, err := s.catalog.Builds()
	if err != nil {
		return nil, err
	}
	resp := &adminv1.ListBuildsResponse{}
	for _, entry := range builds {
		resp.Builds = append(resp.Builds, BuildInfoOf(entry))
	}
	return connect.NewResponse(resp), nil
}

func (s *Service) ListPlayers(ctx context.Context, _ *connect.Request[adminv1.ListPlayersRequest]) (*connect.Response[adminv1.ListPlayersResponse], error) {
	if s.catalog == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("сборки не настроены"))
	}
	list, err := s.catalog.Players(ctx)
	if err != nil {
		return nil, err
	}
	resp := &adminv1.ListPlayersResponse{}
	for i := range list {
		resp.Builds = append(resp.Builds, PlayersInfoOf(&list[i]))
	}
	return connect.NewResponse(resp), nil
}

func (s *Service) Status(_ context.Context, _ *connect.Request[adminv1.StatusRequest]) (*connect.Response[adminv1.StatusResponse], error) {
	var st Status
	if s.status != nil {
		st = s.status()
	}
	response := &adminv1.StatusResponse{
		Version:            st.Version,
		StartedAtUnixNanos: st.StartedAtNano,
		ModulesLoaded:      st.ModulesLoaded,
		MemoryBytes:        st.MemoryBytes,
		Goroutines:         st.Goroutines,
		Capabilities: []adminv1.AdminCapability{
			adminv1.AdminCapability_ADMIN_CAPABILITY_STRUCTURED_ARGUMENTS,
			adminv1.AdminCapability_ADMIN_CAPABILITY_REDACTED_SETTINGS,
			adminv1.AdminCapability_ADMIN_CAPABILITY_SETTINGS_PATCH,
		},
	}
	if s.jobs != nil {
		response.Capabilities = append(response.Capabilities, adminv1.AdminCapability_ADMIN_CAPABILITY_JOBS)
	}
	return connect.NewResponse(response), nil
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
	if req.Msg.Line != "" && len(req.Msg.Arguments) > 0 {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("передайте строку команды или список аргументов, но не оба"))
	}
	arguments := req.Msg.Arguments
	if len(arguments) == 0 {
		parsed, err := command.Split(req.Msg.Line)
		if err != nil {
			return connect.NewError(connect.CodeInvalidArgument, err)
		}
		arguments = parsed
	}
	sender := &streamSender{stream: stream}
	ctx = progress.With(ctx, &streamReporter{sender: sender})
	out := &chunkWriter{sender: sender}
	code := int32(0)
	if err := s.registry.DispatchArgs(ctx, arguments, out); err != nil {
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

func BuildInfoOf(entry BuildEntry) *adminv1.BuildInfo {
	info := &adminv1.BuildInfo{
		SourceRevision: entry.SourceRevision, InputDigest: entry.InputDigest, ReleaseId: entry.ReleaseID, RetainedReleases: entry.RetainedReleases,
		SettingsRevision:          entry.SettingsRevision,
		PublishedSettingsRevision: entry.PublishedSettingsRevision,
		Install:                   entry.Install,
		Name:                      entry.Name,
		Status:                    entry.Status,
		Prepared:                  entry.Prepared,
		Published:                 entry.Published,
		MinecraftVersion:          entry.Minecraft,
		JavaMajor:                 entry.JavaMajor,
		Loader:                    entry.Loader,
		Compat:                    entry.Compat,
		Trouble:                   entry.Trouble,
		SizeBytes:                 entry.SizeBytes,
		Files:                     uint32(entry.Files),
		ServerAddress:             entry.ServerAddress,
		Access:                    entry.Access,
		HasFeatures:               entry.HasFeatures,
	}
	if !entry.PublishedAt.IsZero() {
		info.PublishedAtUnixNanos = entry.PublishedAt.UnixNano()
	}
	return info
}

func PlayersInfoOf(entry *BuildPlayers) *adminv1.BuildPlayers {
	if entry == nil {
		return nil
	}
	return &adminv1.BuildPlayers{
		Build:     entry.Build,
		Address:   entry.Address,
		Reachable: entry.Reachable,
		Online:    entry.Online,
		Max:       entry.Max,
		Names:     entry.Names,
		Version:   entry.Version,
		Error:     entry.Error,
	}
}
