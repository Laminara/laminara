package module

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	modulev1 "github.com/laminara/laminara/sdk/go/gen/laminara/module/v1"
)

var Handshake = goplugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "LAMINARA_MODULE",
	MagicCookieValue: "laminara-module-v1",
}

const PluginName = "module"

func PluginSet(impl Module) goplugin.PluginSet {
	return goplugin.PluginSet{PluginName: &grpcPlugin{impl: impl}}
}

func Serve(impl Module) {
	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins:         PluginSet(impl),
		GRPCServer:      goplugin.DefaultGRPCServer,
	})
}

type grpcPlugin struct {
	goplugin.NetRPCUnsupportedPlugin
	impl Module
}

func (p *grpcPlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	modulev1.RegisterModuleServiceServer(s, &grpcServer{impl: p.impl})
	return nil
}

func (p *grpcPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &grpcClient{client: modulev1.NewModuleServiceClient(c)}, nil
}

type grpcServer struct {
	modulev1.UnimplementedModuleServiceServer
	impl Module

	mu     sync.Mutex
	next   uint64
	opened map[uint64]any
}

func (s *grpcServer) Configure(_ context.Context, req *modulev1.ConfigureRequest) (*modulev1.ConfigureResponse, error) {
	if configurable, ok := s.impl.(Configurable); ok {
		if err := configurable.Configure(req.Config); err != nil {
			return nil, err
		}
	}
	return &modulev1.ConfigureResponse{}, nil
}

func (s *grpcServer) Info(context.Context, *modulev1.InfoRequest) (*modulev1.InfoResponse, error) {
	info := s.impl.Info()
	out := &modulev1.InfoResponse{Name: info.Name, Version: info.Version, Description: info.Description}
	for _, cmd := range s.impl.Commands() {
		out.Commands = append(out.Commands, &modulev1.CommandSpec{Name: cmd.Name, Aliases: cmd.Aliases, Synopsis: cmd.Synopsis})
	}
	if handler, ok := s.impl.(EventHandler); ok {
		out.Events = handler.Events()
	}
	for _, spec := range providerSpecs(s.impl) {
		out.Providers = append(out.Providers, &modulev1.ProviderSpec{Kind: wireKind(spec.Kind), Name: spec.Name})
	}
	return out, nil
}

func (s *grpcServer) Emit(ctx context.Context, req *modulev1.EmitRequest) (*modulev1.EmitResponse, error) {
	if handler, ok := s.impl.(EventHandler); ok {
		if err := handler.OnEvent(ctx, req.Topic, req.Data); err != nil {
			return nil, err
		}
	}
	return &modulev1.EmitResponse{}, nil
}

func (s *grpcServer) Execute(req *modulev1.ExecuteRequest, stream grpc.ServerStreamingServer[modulev1.ExecuteResponse]) error {
	var run func(context.Context, []string, io.Writer) error
	for _, cmd := range s.impl.Commands() {
		if cmd.Name == req.Command {
			run = cmd.Run
			break
		}
	}
	if run == nil {
		return stream.Send(&modulev1.ExecuteResponse{Done: true, Error: "unknown command: " + req.Command})
	}
	err := run(stream.Context(), req.Args, &streamWriter{stream: stream})
	final := &modulev1.ExecuteResponse{Done: true}
	if err != nil {
		final.Error = err.Error()
	}
	return stream.Send(final)
}

func (s *grpcServer) OpenProvider(ctx context.Context, req *modulev1.OpenProviderRequest) (*modulev1.OpenProviderResponse, error) {
	provider, err := s.open(ctx, req)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.opened == nil {
		s.opened = map[uint64]any{}
	}
	s.next++
	s.opened[s.next] = provider
	return &modulev1.OpenProviderResponse{Handle: s.next}, nil
}

func (s *grpcServer) open(ctx context.Context, req *modulev1.OpenProviderRequest) (any, error) {
	switch localKind(req.Kind) {
	case ProviderAuth:
		if set, ok := s.impl.(AuthProviders); ok {
			return set.OpenAuth(ctx, req.Name, req.Config)
		}
	case ProviderSkin:
		if set, ok := s.impl.(SkinProviders); ok {
			return set.OpenSkin(ctx, req.Name, req.Config)
		}
	case ProviderAccess:
		if set, ok := s.impl.(AccessSources); ok {
			return set.OpenAccess(ctx, req.Name, req.Config)
		}
	case ProviderNews:
		if set, ok := s.impl.(NewsSources); ok {
			return set.OpenNews(ctx, req.Name, req.Config)
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, req.Name)
}

func (s *grpcServer) provider(handle uint64) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	provider, ok := s.opened[handle]
	if !ok {
		return nil, fmt.Errorf("provider handle %d was never opened", handle)
	}
	return provider, nil
}

func (s *grpcServer) Authenticate(ctx context.Context, req *modulev1.AuthenticateRequest) (*modulev1.AuthenticateResponse, error) {
	opened, err := s.provider(req.Handle)
	if err != nil {
		return nil, err
	}
	provider, ok := opened.(AuthProvider)
	if !ok {
		return nil, fmt.Errorf("provider handle %d is not an auth provider", req.Handle)
	}
	identity, err := provider.Authenticate(ctx, Credentials{
		Username:      req.Username,
		Password:      req.Password,
		TwoFactorCode: req.TwoFactorCode,
	})
	switch {
	case errors.Is(err, ErrTwoFactorRequired):
		return &modulev1.AuthenticateResponse{Outcome: modulev1.AuthOutcome_AUTH_OUTCOME_TWO_FACTOR_REQUIRED}, nil
	case errors.Is(err, ErrInvalidCredentials):
		return &modulev1.AuthenticateResponse{Outcome: modulev1.AuthOutcome_AUTH_OUTCOME_INVALID_CREDENTIALS}, nil
	case err != nil:
		return nil, err
	}
	return &modulev1.AuthenticateResponse{
		Outcome:  modulev1.AuthOutcome_AUTH_OUTCOME_ACCEPTED,
		Subject:  identity.Subject,
		Username: identity.Username,
		Uuid:     identity.UUID,
	}, nil
}

func (s *grpcServer) Textures(ctx context.Context, req *modulev1.TexturesRequest) (*modulev1.TexturesResponse, error) {
	opened, err := s.provider(req.Handle)
	if err != nil {
		return nil, err
	}
	provider, ok := opened.(SkinProvider)
	if !ok {
		return nil, fmt.Errorf("provider handle %d is not a skin provider", req.Handle)
	}
	textures, err := provider.Textures(ctx, req.Username, req.Uuid)
	if err != nil {
		return nil, err
	}
	return &modulev1.TexturesResponse{SkinUrl: textures.SkinURL, CapeUrl: textures.CapeURL, Slim: textures.Slim}, nil
}

func (s *grpcServer) Allows(ctx context.Context, req *modulev1.AllowsRequest) (*modulev1.AllowsResponse, error) {
	opened, err := s.provider(req.Handle)
	if err != nil {
		return nil, err
	}
	source, ok := opened.(AccessSource)
	if !ok {
		return nil, fmt.Errorf("provider handle %d is not an access source", req.Handle)
	}
	subject := Subject{}
	if req.Subject != nil {
		subject = Subject{Subject: req.Subject.Subject, Username: req.Subject.Username, UUID: req.Subject.Uuid}
	}
	allowed, err := source.Allows(ctx, req.Build, subject)
	if err != nil {
		return nil, err
	}
	return &modulev1.AllowsResponse{Allowed: allowed}, nil
}

func (s *grpcServer) NewsItems(ctx context.Context, req *modulev1.NewsItemsRequest) (*modulev1.NewsItemsResponse, error) {
	opened, err := s.provider(req.Handle)
	if err != nil {
		return nil, err
	}
	source, ok := opened.(NewsSource)
	if !ok {
		return nil, fmt.Errorf("provider handle %d is not a news source", req.Handle)
	}
	items, err := source.Items(ctx)
	if err != nil {
		return nil, err
	}
	out := &modulev1.NewsItemsResponse{Items: make([]*modulev1.NewsItem, 0, len(items))}
	for _, item := range items {
		out.Items = append(out.Items, &modulev1.NewsItem{
			Id:                   item.ID,
			Title:                item.Title,
			Body:                 item.Body,
			PublishedAtUnixNanos: wireTime(item.PublishedAt),
			Tag:                  item.Tag,
			Link:                 item.Link,
			Banner:               item.Banner,
		})
	}
	return out, nil
}

type streamWriter struct {
	stream grpc.ServerStreamingServer[modulev1.ExecuteResponse]
}

func (w *streamWriter) Write(p []byte) (int, error) {
	if err := w.stream.Send(&modulev1.ExecuteResponse{Output: append([]byte(nil), p...)}); err != nil {
		return 0, err
	}
	return len(p), nil
}

type grpcClient struct {
	client modulev1.ModuleServiceClient
}

func (c *grpcClient) Configure(ctx context.Context, config []byte) error {
	_, err := c.client.Configure(ctx, &modulev1.ConfigureRequest{Config: config})
	return err
}

func (c *grpcClient) Info(ctx context.Context) (Manifest, error) {
	resp, err := c.client.Info(ctx, &modulev1.InfoRequest{})
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{
		Info:     Info{Name: resp.Name, Version: resp.Version, Description: resp.Description},
		Commands: make([]CommandSpec, 0, len(resp.Commands)),
		Events:   resp.Events,
	}
	for _, spec := range resp.Commands {
		manifest.Commands = append(manifest.Commands, CommandSpec{Name: spec.Name, Aliases: spec.Aliases, Synopsis: spec.Synopsis})
	}
	for _, spec := range resp.Providers {
		kind := localKind(spec.Kind)
		if kind == 0 {
			continue
		}
		manifest.Providers = append(manifest.Providers, ProviderSpec{Kind: kind, Name: spec.Name})
	}
	return manifest, nil
}

func (c *grpcClient) Emit(ctx context.Context, topic string, data map[string]string) error {
	_, err := c.client.Emit(ctx, &modulev1.EmitRequest{Topic: topic, Data: data})
	return err
}

func (c *grpcClient) Execute(ctx context.Context, command string, args []string, out io.Writer) error {
	stream, err := c.client.Execute(ctx, &modulev1.ExecuteRequest{Command: command, Args: args})
	if err != nil {
		return err
	}
	for {
		chunk, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if len(chunk.Output) > 0 {
			if _, werr := out.Write(chunk.Output); werr != nil {
				return werr
			}
		}
		if chunk.Done {
			if chunk.Error != "" {
				return errors.New(chunk.Error)
			}
			return nil
		}
	}
}

func (c *grpcClient) OpenProvider(ctx context.Context, kind ProviderKind, name string, config []byte) (Handle, error) {
	resp, err := c.client.OpenProvider(ctx, &modulev1.OpenProviderRequest{Kind: wireKind(kind), Name: name, Config: config})
	if err != nil {
		return 0, err
	}
	return Handle(resp.Handle), nil
}

func (c *grpcClient) Authenticate(ctx context.Context, handle Handle, creds Credentials) (Identity, error) {
	resp, err := c.client.Authenticate(ctx, &modulev1.AuthenticateRequest{
		Handle:        uint64(handle),
		Username:      creds.Username,
		Password:      creds.Password,
		TwoFactorCode: creds.TwoFactorCode,
	})
	if err != nil {
		return Identity{}, err
	}
	switch resp.Outcome {
	case modulev1.AuthOutcome_AUTH_OUTCOME_ACCEPTED:
		return Identity{Subject: resp.Subject, Username: resp.Username, UUID: resp.Uuid}, nil
	case modulev1.AuthOutcome_AUTH_OUTCOME_TWO_FACTOR_REQUIRED:
		return Identity{}, ErrTwoFactorRequired
	default:
		return Identity{}, ErrInvalidCredentials
	}
}

func (c *grpcClient) Textures(ctx context.Context, handle Handle, username, uuid string) (Textures, error) {
	resp, err := c.client.Textures(ctx, &modulev1.TexturesRequest{Handle: uint64(handle), Username: username, Uuid: uuid})
	if err != nil {
		return Textures{}, err
	}
	return Textures{SkinURL: resp.SkinUrl, CapeURL: resp.CapeUrl, Slim: resp.Slim}, nil
}

func (c *grpcClient) Allows(ctx context.Context, handle Handle, build string, subject Subject) (bool, error) {
	resp, err := c.client.Allows(ctx, &modulev1.AllowsRequest{
		Handle:  uint64(handle),
		Build:   build,
		Subject: &modulev1.AccessSubject{Subject: subject.Subject, Username: subject.Username, Uuid: subject.UUID},
	})
	if err != nil {
		return false, err
	}
	return resp.Allowed, nil
}

func (c *grpcClient) NewsItems(ctx context.Context, handle Handle) ([]NewsItem, error) {
	resp, err := c.client.NewsItems(ctx, &modulev1.NewsItemsRequest{Handle: uint64(handle)})
	if err != nil {
		return nil, err
	}
	items := make([]NewsItem, 0, len(resp.Items))
	for _, item := range resp.Items {
		items = append(items, NewsItem{
			ID:          item.Id,
			Title:       item.Title,
			Body:        item.Body,
			PublishedAt: localTime(item.PublishedAtUnixNanos),
			Tag:         item.Tag,
			Link:        item.Link,
			Banner:      item.Banner,
		})
	}
	return items, nil
}

func wireKind(kind ProviderKind) modulev1.ProviderKind {
	switch kind {
	case ProviderAuth:
		return modulev1.ProviderKind_PROVIDER_KIND_AUTH
	case ProviderSkin:
		return modulev1.ProviderKind_PROVIDER_KIND_SKIN
	case ProviderAccess:
		return modulev1.ProviderKind_PROVIDER_KIND_ACCESS
	case ProviderNews:
		return modulev1.ProviderKind_PROVIDER_KIND_NEWS
	default:
		return modulev1.ProviderKind_PROVIDER_KIND_UNSPECIFIED
	}
}

func localKind(kind modulev1.ProviderKind) ProviderKind {
	switch kind {
	case modulev1.ProviderKind_PROVIDER_KIND_AUTH:
		return ProviderAuth
	case modulev1.ProviderKind_PROVIDER_KIND_SKIN:
		return ProviderSkin
	case modulev1.ProviderKind_PROVIDER_KIND_ACCESS:
		return ProviderAccess
	case modulev1.ProviderKind_PROVIDER_KIND_NEWS:
		return ProviderNews
	default:
		return 0
	}
}

func wireTime(at time.Time) int64 {
	if at.IsZero() {
		return 0
	}
	return at.UnixNano()
}

func localTime(nanos int64) time.Time {
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos).UTC()
}
