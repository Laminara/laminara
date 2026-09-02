package module

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"

	modulev1 "github.com/laminara/laminara/sdk/go/gen/laminara/module/v1"
)

type testModule struct{}

func (testModule) Info() Info {
	return Info{Name: "test", Version: "9.9", Description: "тест"}
}

func (testModule) Commands() []Command {
	return []Command{
		{
			Name:     "greet",
			Aliases:  []string{"hi"},
			Synopsis: "greet",
			Run: func(_ context.Context, args []string, out io.Writer) error {
				fmt.Fprintf(out, "hello %s", strings.Join(args, " "))
				return nil
			},
		},
		{
			Name: "boom",
			Run: func(context.Context, []string, io.Writer) error {
				return fmt.Errorf("kaboom")
			},
		},
	}
}

type fakeStream struct {
	grpc.ServerStream
	ctx    context.Context
	chunks []*modulev1.ExecuteResponse
}

func (f *fakeStream) Send(c *modulev1.ExecuteResponse) error {
	f.chunks = append(f.chunks, c)
	return nil
}

func (f *fakeStream) Context() context.Context { return f.ctx }

func (f *fakeStream) output() string {
	var buf bytes.Buffer
	for _, c := range f.chunks {
		buf.Write(c.Output)
	}
	return buf.String()
}

func (f *fakeStream) final() *modulev1.ExecuteResponse {
	for _, c := range f.chunks {
		if c.Done {
			return c
		}
	}
	return nil
}

type configurableModule struct{ got string }

func (c *configurableModule) Info() Info                    { return Info{Name: "cfg", Version: "1"} }
func (c *configurableModule) Commands() []Command           { return nil }
func (c *configurableModule) Configure(config []byte) error { c.got = string(config); return nil }

func TestServerAdapterConfigure(t *testing.T) {
	m := &configurableModule{}
	srv := &grpcServer{impl: m}
	if _, err := srv.Configure(context.Background(), &modulev1.ConfigureRequest{Config: []byte(`{"greeting":"Здорово"}`)}); err != nil {
		t.Fatal(err)
	}
	if m.got != `{"greeting":"Здорово"}` {
		t.Fatalf("config not delivered: %q", m.got)
	}

	plain := &grpcServer{impl: testModule{}}
	if _, err := plain.Configure(context.Background(), &modulev1.ConfigureRequest{Config: []byte(`{}`)}); err != nil {
		t.Fatalf("non-configurable module must accept Configure as a no-op: %v", err)
	}
}

type eventModule struct{ got []string }

func (e *eventModule) Info() Info          { return Info{Name: "ev", Version: "1"} }
func (e *eventModule) Commands() []Command { return nil }
func (e *eventModule) Events() []string    { return []string{"build.published"} }
func (e *eventModule) OnEvent(_ context.Context, topic string, data map[string]string) error {
	e.got = append(e.got, topic+":"+data["name"])
	return nil
}

func TestServerAdapterEvents(t *testing.T) {
	m := &eventModule{}
	srv := &grpcServer{impl: m}

	info, err := srv.Info(context.Background(), &modulev1.InfoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Events) != 1 || info.Events[0] != "build.published" {
		t.Fatalf("declared events = %v", info.Events)
	}

	if _, err := srv.Emit(context.Background(), &modulev1.EmitRequest{Topic: "build.published", Data: map[string]string{"name": "Survival"}}); err != nil {
		t.Fatal(err)
	}
	if len(m.got) != 1 || m.got[0] != "build.published:Survival" {
		t.Fatalf("delivered events = %v", m.got)
	}

	plain := &grpcServer{impl: testModule{}}
	pInfo, _ := plain.Info(context.Background(), &modulev1.InfoRequest{})
	if len(pInfo.Events) != 0 {
		t.Fatalf("non-handler must declare no events, got %v", pInfo.Events)
	}
	if _, err := plain.Emit(context.Background(), &modulev1.EmitRequest{Topic: "build.published"}); err != nil {
		t.Fatalf("non-handler Emit must be a no-op: %v", err)
	}
}

func TestServerAdapterInfo(t *testing.T) {
	srv := &grpcServer{impl: testModule{}}
	info, err := srv.Info(context.Background(), &modulev1.InfoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "test" || len(info.Commands) != 2 {
		t.Fatalf("info = %+v", info)
	}
	if info.Commands[0].Name != "greet" || len(info.Commands[0].Aliases) != 1 {
		t.Fatalf("command spec = %+v", info.Commands[0])
	}
}

type providerModule struct {
	openedWith string
}

func (p *providerModule) Info() Info          { return Info{Name: "prov", Version: "1"} }
func (p *providerModule) Commands() []Command { return nil }

func (p *providerModule) AuthProviders() []string { return []string{"demo"} }

func (p *providerModule) OpenAuth(_ context.Context, name string, config []byte) (AuthProvider, error) {
	if name != "demo" {
		return nil, ErrUnknownProvider
	}
	p.openedWith = string(config)
	return demoAuth{}, nil
}

func (p *providerModule) NewsSources() []string { return []string{"demo"} }

func (p *providerModule) OpenNews(context.Context, string, []byte) (NewsSource, error) {
	return demoNews{}, nil
}

type demoAuth struct{}

func (demoAuth) Authenticate(_ context.Context, creds Credentials) (Identity, error) {
	switch {
	case creds.Password != "secret":
		return Identity{}, ErrInvalidCredentials
	case creds.TwoFactorCode == "":
		return Identity{}, ErrTwoFactorRequired
	}
	return Identity{Subject: "42", Username: creds.Username, UUID: "d1b0f4a2-0000-4000-8000-000000000001"}, nil
}

type demoNews struct{}

func (demoNews) Items(context.Context) ([]NewsItem, error) {
	return []NewsItem{{ID: "1", Title: "Открытие", PublishedAt: time.Unix(1700000000, 0).UTC()}}, nil
}

func TestServerAdapterProviders(t *testing.T) {
	m := &providerModule{}
	srv := &grpcServer{impl: m}
	ctx := context.Background()

	info, err := srv.Info(ctx, &modulev1.InfoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Providers) != 2 {
		t.Fatalf("declared providers = %+v", info.Providers)
	}
	if info.Providers[0].Kind != modulev1.ProviderKind_PROVIDER_KIND_AUTH || info.Providers[0].Name != "demo" {
		t.Fatalf("auth spec = %+v", info.Providers[0])
	}
	if info.Providers[1].Kind != modulev1.ProviderKind_PROVIDER_KIND_NEWS {
		t.Fatalf("news spec = %+v", info.Providers[1])
	}

	opened, err := srv.OpenProvider(ctx, &modulev1.OpenProviderRequest{
		Kind:   modulev1.ProviderKind_PROVIDER_KIND_AUTH,
		Name:   "demo",
		Config: []byte(`{"url":"https://cms"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.openedWith != `{"url":"https://cms"}` {
		t.Fatalf("provider config not delivered: %q", m.openedWith)
	}

	accepted, err := srv.Authenticate(ctx, &modulev1.AuthenticateRequest{
		Handle: opened.Handle, Username: "neo", Password: "secret", TwoFactorCode: "654321",
	})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Outcome != modulev1.AuthOutcome_AUTH_OUTCOME_ACCEPTED || accepted.Username != "neo" {
		t.Fatalf("accepted = %+v", accepted)
	}

	second, err := srv.Authenticate(ctx, &modulev1.AuthenticateRequest{Handle: opened.Handle, Username: "neo", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Outcome != modulev1.AuthOutcome_AUTH_OUTCOME_TWO_FACTOR_REQUIRED {
		t.Fatalf("second factor outcome = %v", second.Outcome)
	}

	wrong, err := srv.Authenticate(ctx, &modulev1.AuthenticateRequest{Handle: opened.Handle, Username: "neo", Password: "nope"})
	if err != nil {
		t.Fatal(err)
	}
	if wrong.Outcome != modulev1.AuthOutcome_AUTH_OUTCOME_INVALID_CREDENTIALS {
		t.Fatalf("wrong password outcome = %v", wrong.Outcome)
	}

	if _, err := srv.Textures(ctx, &modulev1.TexturesRequest{Handle: opened.Handle}); err == nil {
		t.Fatal("an auth handle must not answer texture calls")
	}
	if _, err := srv.Authenticate(ctx, &modulev1.AuthenticateRequest{Handle: opened.Handle + 100}); err == nil {
		t.Fatal("an unopened handle must be refused")
	}

	news, err := srv.OpenProvider(ctx, &modulev1.OpenProviderRequest{Kind: modulev1.ProviderKind_PROVIDER_KIND_NEWS, Name: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	items, err := srv.NewsItems(ctx, &modulev1.NewsItemsRequest{Handle: news.Handle})
	if err != nil {
		t.Fatal(err)
	}
	if len(items.Items) != 1 || items.Items[0].PublishedAtUnixNanos != time.Unix(1700000000, 0).UnixNano() {
		t.Fatalf("news items = %+v", items.Items)
	}

	if _, err := srv.OpenProvider(ctx, &modulev1.OpenProviderRequest{Kind: modulev1.ProviderKind_PROVIDER_KIND_SKIN, Name: "demo"}); err == nil {
		t.Fatal("a module without skin providers must refuse to open one")
	}
}

func TestServerAdapterExecute(t *testing.T) {
	srv := &grpcServer{impl: testModule{}}

	ok := &fakeStream{ctx: context.Background()}
	if err := srv.Execute(&modulev1.ExecuteRequest{Command: "greet", Args: []string{"world"}}, ok); err != nil {
		t.Fatal(err)
	}
	if ok.output() != "hello world" {
		t.Fatalf("output = %q", ok.output())
	}
	if f := ok.final(); f == nil || f.Error != "" {
		t.Fatalf("final = %+v", f)
	}

	fail := &fakeStream{ctx: context.Background()}
	if err := srv.Execute(&modulev1.ExecuteRequest{Command: "boom"}, fail); err != nil {
		t.Fatal(err)
	}
	if f := fail.final(); f == nil || f.Error != "kaboom" {
		t.Fatalf("expected error propagated, final = %+v", f)
	}

	unknown := &fakeStream{ctx: context.Background()}
	if err := srv.Execute(&modulev1.ExecuteRequest{Command: "nope"}, unknown); err != nil {
		t.Fatal(err)
	}
	if f := unknown.final(); f == nil || !strings.Contains(f.Error, "unknown command") {
		t.Fatalf("expected unknown-command error, final = %+v", f)
	}
}
