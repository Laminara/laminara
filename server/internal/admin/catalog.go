package admin

import (
	"context"
	"time"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
)

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
	Name          string
	Status        string
	Minecraft     string
	JavaMajor     uint32
	Loader        string
	SizeBytes     uint64
	Files         int
	ServerAddress string
	Access        string
	HasFeatures   bool
	Prepared      []corev1.Platform
	Published     []corev1.Platform
	PublishedAt   time.Time
}

type BuildPlayers struct {
	Build     string
	Address   string
	Reachable bool
	Online    int64
	Max       int64
	Names     []string
	Version   string
	Error     string
}

type Catalog interface {
	Versions(ctx context.Context, query string) (VersionList, error)
	Loaders(ctx context.Context, mcVersion string) ([]LoaderEntry, error)
	Builds() ([]BuildEntry, error)
	Players(ctx context.Context) ([]BuildPlayers, error)
}
