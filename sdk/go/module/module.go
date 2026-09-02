package module

import (
	"context"
	"errors"
	"io"
	"time"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrTwoFactorRequired  = errors.New("two-factor code required")
	ErrUnknownProvider    = errors.New("unknown provider")
)

type Info struct {
	Name        string
	Version     string
	Description string
}

type Command struct {
	Name     string
	Aliases  []string
	Synopsis string
	Run      func(ctx context.Context, args []string, out io.Writer) error
}

type CommandSpec struct {
	Name     string
	Aliases  []string
	Synopsis string
}

type ProviderKind int

const (
	ProviderAuth ProviderKind = iota + 1
	ProviderSkin
	ProviderAccess
	ProviderNews
)

type ProviderSpec struct {
	Kind ProviderKind
	Name string
}

type Manifest struct {
	Info      Info
	Commands  []CommandSpec
	Events    []string
	Providers []ProviderSpec
}

type Module interface {
	Info() Info
	Commands() []Command
}

type Configurable interface {
	Configure(config []byte) error
}

type EventHandler interface {
	Events() []string
	OnEvent(ctx context.Context, topic string, data map[string]string) error
}

type Credentials struct {
	Username      string
	Password      string
	TwoFactorCode string
}

type Identity struct {
	Subject  string
	Username string
	UUID     string
}

type Textures struct {
	SkinURL string
	CapeURL string
	Slim    bool
}

type Subject struct {
	Subject  string
	Username string
	UUID     string
}

type NewsItem struct {
	ID          string
	Title       string
	Body        string
	PublishedAt time.Time
	Tag         string
	Link        string
	Banner      string
}

type AuthProvider interface {
	Authenticate(ctx context.Context, creds Credentials) (Identity, error)
}

type AuthProviders interface {
	AuthProviders() []string
	OpenAuth(ctx context.Context, name string, config []byte) (AuthProvider, error)
}

type SkinProvider interface {
	Textures(ctx context.Context, username, uuid string) (Textures, error)
}

type SkinProviders interface {
	SkinProviders() []string
	OpenSkin(ctx context.Context, name string, config []byte) (SkinProvider, error)
}

type AccessSource interface {
	Allows(ctx context.Context, build string, subject Subject) (bool, error)
}

type AccessSources interface {
	AccessSources() []string
	OpenAccess(ctx context.Context, name string, config []byte) (AccessSource, error)
}

type NewsSource interface {
	Items(ctx context.Context) ([]NewsItem, error)
}

type NewsSources interface {
	NewsSources() []string
	OpenNews(ctx context.Context, name string, config []byte) (NewsSource, error)
}

type Handle uint64

type Service interface {
	Configure(ctx context.Context, config []byte) error
	Info(ctx context.Context) (Manifest, error)
	Execute(ctx context.Context, command string, args []string, out io.Writer) error
	Emit(ctx context.Context, topic string, data map[string]string) error
	OpenProvider(ctx context.Context, kind ProviderKind, name string, config []byte) (Handle, error)
	Authenticate(ctx context.Context, handle Handle, creds Credentials) (Identity, error)
	Textures(ctx context.Context, handle Handle, username, uuid string) (Textures, error)
	Allows(ctx context.Context, handle Handle, build string, subject Subject) (bool, error)
	NewsItems(ctx context.Context, handle Handle) ([]NewsItem, error)
}

func providerSpecs(impl Module) []ProviderSpec {
	var specs []ProviderSpec
	if set, ok := impl.(AuthProviders); ok {
		specs = append(specs, named(ProviderAuth, set.AuthProviders())...)
	}
	if set, ok := impl.(SkinProviders); ok {
		specs = append(specs, named(ProviderSkin, set.SkinProviders())...)
	}
	if set, ok := impl.(AccessSources); ok {
		specs = append(specs, named(ProviderAccess, set.AccessSources())...)
	}
	if set, ok := impl.(NewsSources); ok {
		specs = append(specs, named(ProviderNews, set.NewsSources())...)
	}
	return specs
}

func named(kind ProviderKind, names []string) []ProviderSpec {
	specs := make([]ProviderSpec, 0, len(names))
	for _, name := range names {
		specs = append(specs, ProviderSpec{Kind: kind, Name: name})
	}
	return specs
}
