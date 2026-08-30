package catalog

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/platform"
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

func ManifestName(build string, p corev1.Platform) string {
	if key, ok := platform.Key(p); ok {
		return build + "." + key + manifestExt
	}
	return build + manifestExt
}

func ManifestPath(dir, build string, p corev1.Platform) string {
	return filepath.Join(dir, ManifestName(build, p))
}

func SignaturePath(manifest string) string {
	return strings.TrimSuffix(manifest, manifestExt) + signatureExt
}

type Catalog struct {
	dir string

	mu      sync.Mutex
	current *snapshot
	checked time.Time

	rescan sync.Mutex
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

func (c *Catalog) snapshot() *snapshot {
	if snap := c.ready(); snap != nil {
		return snap
	}

	c.rescan.Lock()
	defer c.rescan.Unlock()

	if snap := c.ready(); snap != nil {
		return snap
	}

	started := time.Now()
	stamp, entries, err := c.index()
	current := c.state()
	if err != nil {
		slog.Warn("каталог: не удалось перечитать папку сборок", "source", "catalog", "проект", c.dir, "ошибка", err)
		if current != nil {
			return current
		}
		return &snapshot{builds: map[string][]variant{}, owners: map[string][]string{}}
	}
	defer c.scanned(started)

	if current != nil && current.stamp == stamp {
		return current
	}

	next := c.collect(stamp, entries)
	c.mu.Lock()
	c.current = next
	c.mu.Unlock()
	return next
}

func (c *Catalog) ready() *snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current == nil || time.Since(c.checked) >= rescanInterval {
		return nil
	}
	return c.current
}

func (c *Catalog) state() *snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

func (c *Catalog) scanned(started time.Time) {
	c.mu.Lock()
	if c.checked.Before(started) {
		c.checked = started
	}
	c.mu.Unlock()
}

func (c *Catalog) index() (string, []string, error) {
	listed, err := os.ReadDir(c.dir)
	if err != nil {
		slog.Warn("каталог: папку с манифестами открыть не вышло", "source", "catalog", "проект", c.dir, "ошибка", err)
		return "", nil, err
	}

	matches := make([]string, 0, len(listed))
	for _, entry := range listed {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), manifestExt) {
			continue
		}
		matches = append(matches, filepath.Join(c.dir, entry.Name()))
	}
	sort.Strings(matches)

	entries := make([]string, 0, len(matches))
	var stamp strings.Builder
	for _, entry := range matches {
		info, err := os.Stat(entry)
		if err != nil {
			slog.Warn("каталог: манифест недоступен", "source", "catalog", "файл", filepath.Base(entry), "ошибка", err)
			continue
		}
		entries = append(entries, entry)
		stamp.WriteString(entry)
		stamp.WriteByte(0)
		stamp.WriteString(strconv.FormatInt(info.ModTime().UnixNano(), 10))
		stamp.WriteByte(0)
		stamp.WriteString(strconv.FormatInt(info.Size(), 10))
		stamp.WriteByte('\n')
	}
	return stamp.String(), entries, nil
}

func (c *Catalog) collect(stamp string, entries []string) *snapshot {
	next := &snapshot{stamp: stamp, builds: map[string][]variant{}, owners: map[string][]string{}}
	for _, entry := range entries {
		canonical, err := os.ReadFile(entry)
		if err != nil {
			slog.Warn("каталог: манифест не прочитан", "source", "catalog", "файл", filepath.Base(entry), "ошибка", err)
			continue
		}
		var m corev1.Manifest
		if err := proto.Unmarshal(canonical, &m); err != nil {
			slog.Warn("каталог: манифест повреждён, сборка пропущена", "source", "catalog", "файл", filepath.Base(entry), "ошибка", err)
			continue
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
	return next
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
	c.checked = time.Time{}
	c.mu.Unlock()
}

func (c *Catalog) List() ([]string, error) {
	return append([]string(nil), c.snapshot().names...), nil
}

func (c *Catalog) Owners(key string) ([]string, error) {
	return c.snapshot().owners[key], nil
}

func (c *Catalog) Get(name string, want corev1.Platform) (canonical, signature []byte, err error) {
	list, ok := c.snapshot().builds[name]
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
	signature, err = os.ReadFile(SignaturePath(chosen.file))
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
	snap := c.snapshot()

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
	snap := c.snapshot()
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
