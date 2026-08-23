package admin

import "context"

type VersionEntry struct {
	ID   string
	Type string
}

type VersionList struct {
	Versions       []VersionEntry
	LatestRelease  string
	LatestSnapshot string
}

type LoaderEntry struct {
	Name     string
	Versions []string
}

type BuildEntry struct {
	Name   string
	Status string
}

type Catalog interface {
	Versions(ctx context.Context, query string) (VersionList, error)
	Loaders(ctx context.Context, mcVersion string) ([]LoaderEntry, error)
	Builds() ([]BuildEntry, error)
}
