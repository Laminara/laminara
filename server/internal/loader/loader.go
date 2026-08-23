package loader

import (
	"context"
	"sort"
)

type Library struct {
	Name string
	URL  string
}

type LoaderProfile struct {
	MainClass string
	Libraries []Library
}

type Loader interface {
	Name() string
	Versions(ctx context.Context, mcVersion string) ([]string, error)
	Resolve(ctx context.Context, mcVersion, loaderVersion string) (*LoaderProfile, error)
}

type Downloader func(ctx context.Context, url, dest, sha1 string) error

type InstallRequest struct {
	MCVersion     string
	LoaderVersion string
	ProfileDir    string
	LibrariesDir  string
	MinecraftJar  string
	JavaBin       string
	Download      Downloader
}

type InstallResult struct {
	MainClass string
	JVMArgs   []string
	GameArgs  []string
	Libraries []string
	ClientJar string
}

type Installer interface {
	Install(ctx context.Context, req InstallRequest) (*InstallResult, error)
}

var registry = map[string]Loader{}

func Register(l Loader) {
	registry[l.Name()] = l
}

func Get(name string) (Loader, bool) {
	l, ok := registry[name]
	return l, ok
}

func All() []Loader {
	out := make([]Loader, 0, len(registry))
	for _, l := range registry {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}
