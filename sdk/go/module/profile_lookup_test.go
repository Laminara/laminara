package module

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	modulev1 "github.com/laminara/laminara/sdk/go/gen/laminara/module/v1"
)

type lookupAuth struct{ demoAuth }

func (lookupAuth) LookupProfile(_ context.Context, username string) (Identity, error) {
	if username == "missing" {
		return Identity{}, ErrInvalidCredentials
	}
	if username == "outage" {
		return Identity{}, errors.New("profile source unavailable")
	}
	return Identity{Subject: "42", Username: "Neo", UUID: "d1b0f4a2-0000-4000-8000-000000000001"}, nil
}

func TestProfileLookupOverGRPC(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	modulev1.RegisterModuleServiceServer(server, &grpcServer{opened: map[uint64]any{1: lookupAuth{}, 2: demoAuth{}, 3: demoNews{}}})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	t.Cleanup(func() { _ = listener.Close() })
	conn, err := grpc.NewClient("passthrough:///module-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := &grpcClient{client: modulev1.NewModuleServiceClient(conn)}
	identity, err := client.LookupProfile(t.Context(), 1, "neo")
	if err != nil || identity.Username != "Neo" || identity.Subject != "42" || identity.UUID != "d1b0f4a2-0000-4000-8000-000000000001" {
		t.Fatalf("lookup: %+v %v", identity, err)
	}
	if _, err := client.LookupProfile(t.Context(), 1, "missing"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown profile: %v", err)
	}
	if _, err := client.LookupProfile(t.Context(), 1, "outage"); err == nil || !strings.Contains(err.Error(), "source unavailable") {
		t.Fatalf("outage: %v", err)
	}
	if _, err := client.LookupProfile(t.Context(), 2, "neo"); status.Code(err) != codes.Unimplemented {
		t.Fatalf("legacy module: %v", err)
	}
	for _, handle := range []Handle{3, 99} {
		if _, err := client.LookupProfile(t.Context(), handle, "neo"); err == nil {
			t.Fatalf("invalid auth handle accepted: %d", handle)
		}
	}
}
