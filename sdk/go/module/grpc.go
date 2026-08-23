package module

import (
	"context"
	"errors"
	"io"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	modulev1 "github.com/laminara/laminara/gen/go/laminara/module/v1"
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

func (c *grpcClient) Info(ctx context.Context) (Info, []CommandSpec, []string, error) {
	resp, err := c.client.Info(ctx, &modulev1.InfoRequest{})
	if err != nil {
		return Info{}, nil, nil, err
	}
	specs := make([]CommandSpec, 0, len(resp.Commands))
	for _, spec := range resp.Commands {
		specs = append(specs, CommandSpec{Name: spec.Name, Aliases: spec.Aliases, Synopsis: spec.Synopsis})
	}
	return Info{Name: resp.Name, Version: resp.Version, Description: resp.Description}, specs, resp.Events, nil
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
