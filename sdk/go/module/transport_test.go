package module

import (
	"context"
	"errors"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	modulev1 "github.com/laminara/laminara/sdk/go/gen/laminara/module/v1"
)

func dialModule(t *testing.T, impl Module) Service {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	modulev1.RegisterModuleServiceServer(server, &grpcServer{impl: impl})
	go func() { _ = server.Serve(listener) }()

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		server.Stop()
	})
	return &grpcClient{client: modulev1.NewModuleServiceClient(conn)}
}

func TestProvidersOverTheWire(t *testing.T) {
	ctx := context.Background()
	svc := dialModule(t, &providerModule{})

	manifest, err := svc.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Providers) != 2 || manifest.Providers[0].Kind != ProviderAuth {
		t.Fatalf("manifest providers = %+v", manifest.Providers)
	}

	handle, err := svc.OpenProvider(ctx, ProviderAuth, "demo", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := svc.Authenticate(ctx, handle, Credentials{Username: "neo", Password: "secret", TwoFactorCode: "654321"})
	if err != nil {
		t.Fatal(err)
	}
	if identity.Username != "neo" || identity.Subject != "42" {
		t.Fatalf("identity = %+v", identity)
	}
	if _, err := svc.Authenticate(ctx, handle, Credentials{Username: "neo", Password: "secret"}); !errors.Is(err, ErrTwoFactorRequired) {
		t.Fatalf("two-factor outcome did not survive the wire: %v", err)
	}
	if _, err := svc.Authenticate(ctx, handle, Credentials{Username: "neo", Password: "nope"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("invalid credentials did not survive the wire: %v", err)
	}

	news, err := svc.OpenProvider(ctx, ProviderNews, "demo", nil)
	if err != nil {
		t.Fatal(err)
	}
	items, err := svc.NewsItems(ctx, news)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "Открытие" || items[0].PublishedAt.IsZero() {
		t.Fatalf("news items = %+v", items)
	}

	if _, err := svc.OpenProvider(ctx, ProviderAccess, "demo", nil); err == nil {
		t.Fatal("opening a provider the module does not have must fail")
	}
}
