package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/storage"
)

const (
	manifestExt    = ".manifest"
	signatureExt   = ".manifest.sig"
	rescanInterval = 2 * time.Second
)

var (
	ErrNotFound            = errors.New("profile not found")
	ErrPlatformUnavailable = errors.New("build is not available for this platform")
)

type Catalog struct {
	dir string

	mu      sync.Mutex
	current *snapshot
	checked time.Time
}

func New(dir string) *Catalog {
	return &Catalog{dir: dir}
}

type variant struct {
	file     string
	platform corev1.Platform
	manifest *corev1.Manifest
}

type snapshot struct {
	stamp  string
	builds map[string][]variant
	names  []string
	owners map[string][]string
}

func (c *Catalog) stamp() (string, []string, error) {
	entries, err := filepath.Glob(filepath.Join(c.dir, "*"+manifestExt))
	if err != nil {
		return "", nil, err
	}
	sort.Strings(entries)
	var builder strings.Builder
	for _, entry := range entries {
		info, err := os.Stat(entry)
		if err != nil {
			return "", nil, err
		}
		builder.WriteString(entry)
		builder.WriteByte(0)
		builder.WriteString(strconv.FormatInt(info.ModTime().UnixNano(), 10))
		builder.WriteByte(0)
		builder.WriteString(strconv.FormatInt(info.Size(), 10))
		builder.WriteByte('\n')
	}
	return builder.String(), entries, nil
}

func (c *Catalog) snapshot() (*snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if c.current != nil && now.Sub(c.checked) < rescanInterval {
		return c.current, nil
	}
	c.checked = now

	stamp, entries, err := c.stamp()
	if err != nil {
		return nil, err
	}
	if c.current != nil && c.current.stamp == stamp {
		return c.current, nil
	}

	next := &snapshot{stamp: stamp, builds: map[string][]variant{}, owners: map[string][]string{}}
	for _, entry := range entries {
		canonical, err := os.ReadFile(entry)
		if err != nil {
			return nil, err
		}
		var m corev1.Manifest
		if err := proto.Unmarshal(canonical, &m); err != nil {
			return nil, err
		}
		name := m.Modpack
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(entry), manifestExt)
		}
		next.builds[name] = append(next.builds[name], variant{file: entry, platform: m.Platform, manifest: &m})
	}
	for name, list := range next.builds {
		sort.Slice(list, func(i, j int) bool { return list[i].platform < list[j].platform })
		next.names = append(next.names, name)
	}
	sort.Strings(next.names)
	next.indexOwners()

	c.current = next
	return next, nil
}

func (s *snapshot) indexOwners() {
	for _, name := range s.names {
		for _, v := range s.builds[name] {
			for _, file := range v.manifest.Files {
				if file.Object == nil || file.Object.Hash == nil {
					continue
				}
				key := storage.ObjectKey(file.Object.Hash.Algo, file.Object.Hash.Value)
				owners := s.owners[key]
				if len(owners) > 0 && owners[len(owners)-1] == name {
					continue
				}
				s.owners[key] = append(owners, name)
			}
		}
	}
}

func (c *Catalog) Refresh() {
	c.mu.Lock()
	c.current, c.checked = nil, time.Time{}
	c.mu.Unlock()
}

func (c *Catalog) List() ([]string, error) {
	snap, err := c.snapshot()
	if err != nil {
		return nil, err
	}
	return append([]string(nil), snap.names...), nil
}

func (c *Catalog) Owners(key string) ([]string, error) {
	snap, err := c.snapshot()
	if err != nil {
		return nil, err
	}
	return snap.owners[key], nil
}

func (c *Catalog) Get(name string, want corev1.Platform) (canonical, signature []byte, err error) {
	snap, err := c.snapshot()
	if err != nil {
		return nil, nil, err
	}
	list, ok := snap.builds[name]
	if !ok || len(list) == 0 {
		return nil, nil, ErrNotFound
	}

	chosen := pick(list, want)
	if chosen == nil {
		return nil, nil, ErrPlatformUnavailable
	}
	canonical, err = os.ReadFile(chosen.file)
	if err != nil {
		return nil, nil, err
	}
	signature, err = os.ReadFile(strings.TrimSuffix(chosen.file, manifestExt) + signatureExt)
	if err != nil {
		return nil, nil, err
	}
	return canonical, signature, nil
}

func pick(list []variant, want corev1.Platform) *variant {
	for i := range list {
		if list[i].platform == want && want != corev1.Platform_PLATFORM_UNSPECIFIED {
			return &list[i]
		}
	}
	for i := range list {
		if list[i].platform == corev1.Platform_PLATFORM_UNSPECIFIED {
			return &list[i]
		}
	}
	return nil
}

type Summary struct {
	Name          string
	Version       string
	Minecraft     string
	TotalSize     uint64
	ServerAddress string
	Loader        string
	HasFeatures   bool
	Platforms     []corev1.Platform
}

func (c *Catalog) Summaries(want corev1.Platform) ([]Summary, error) {
	snap, err := c.snapshot()
	if err != nil {
		return nil, err
	}

	summaries := make([]Summary, 0, len(snap.names))
	for _, name := range snap.names {
		list := snap.builds[name]
		supported := make([]corev1.Platform, 0, len(list))
		for i := range list {
			if list[i].platform != corev1.Platform_PLATFORM_UNSPECIFIED {
				supported = append(supported, list[i].platform)
			}
		}

		chosen := pick(list, want)
		if chosen == nil {
			chosen = &list[0]
		}
		m := chosen.manifest
		summaries = append(summaries, Summary{
			Name:          name,
			Version:       m.Version,
			Minecraft:     m.MinecraftVersion,
			TotalSize:     m.TotalSize,
			ServerAddress: m.ServerAddress,
			Loader:        m.Loader,
			HasFeatures:   m.Features != nil && len(m.Features.Groups) > 0,
			Platforms:     supported,
		})
	}
	return summaries, nil
}

type Variant struct {
	Platform      corev1.Platform
	Minecraft     string
	JavaMajor     uint32
	Loader        string
	TotalSize     uint64
	Files         int
	ServerAddress string
	HasFeatures   bool
	PublishedAt   time.Time
}

type Detail struct {
	Name     string
	Variants []Variant
}

func (c *Catalog) Details() ([]Detail, error) {
	snap, err := c.snapshot()
	if err != nil {
		return nil, err
	}
	details := make([]Detail, 0, len(snap.names))
	for _, name := range snap.names {
		detail := Detail{Name: name}
		for _, v := range snap.builds[name] {
			m := v.manifest
			detail.Variants = append(detail.Variants, Variant{
				Platform:      v.platform,
				Minecraft:     m.MinecraftVersion,
				JavaMajor:     m.JavaMajor,
				Loader:        m.Loader,
				TotalSize:     m.TotalSize,
				Files:         len(m.Files),
				ServerAddress: m.ServerAddress,
				HasFeatures:   m.Features != nil && len(m.Features.Groups) > 0,
				PublishedAt:   time.Unix(0, m.GeneratedAtUnixNanos),
			})
		}
		details = append(details, detail)
	}
	return details, nil
}
