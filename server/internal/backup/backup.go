package backup

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/version"
)

const (
	manifestName = "manifest.json"
	filesPrefix  = "files/"
	maxFileBytes = 512 << 20
)

type Item struct {
	Name string `json:"name"`
	Path string `json:"path"`
	What string `json:"what"`
	Mode uint32 `json:"mode"`
}

type Manifest struct {
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	Items     []Item    `json:"items"`
	Skipped   []string  `json:"skipped"`
}

type source struct {
	path string
	what string
}

func sources(cfg *config.Config, configPath string) []source {
	var found []source
	add := func(path, what string) {
		if strings.TrimSpace(path) == "" {
			return
		}
		found = append(found, source{path: path, what: what})
	}

	add(configPath, "настройки сервера")
	if cfg.Build != nil {
		add(cfg.Build.SigningKeyPath, "ключ подписи сборок")
		for _, trusted := range cfg.Build.TrustedSigningKeys {
			if looksLikePath(trusted) {
				add(trusted, "отставной ключ подписи")
			}
		}
	}
	if cfg.Yggdrasil != nil {
		add(cfg.Yggdrasil.RSAKeyPath, "ключ входа в игре")
	}
	if cfg.HWID != nil {
		add(cfg.HWID.SaltPath, "соль отпечатков")
		add(cfg.HWID.TicketSecretPath, "ключ пропусков")
		add(sqlitePath(cfg.HWID.Store.Config), "база компьютеров и банов")
	}
	return found
}

func looksLikePath(value string) bool {
	return strings.ContainsAny(value, "/\\") || strings.HasSuffix(value, ".hex")
}

func sqlitePath(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var store struct {
		Driver string `json:"driver"`
		DSN    string `json:"dsn"`
	}
	if err := json.Unmarshal(raw, &store); err != nil {
		return ""
	}
	if store.Driver != "sqlite" && store.Driver != "" {
		return ""
	}
	dsn := store.DSN
	if dsn == "" || strings.Contains(dsn, ":memory:") {
		return ""
	}
	if cut := strings.IndexAny(dsn, "?"); cut >= 0 {
		dsn = dsn[:cut]
	}
	return strings.TrimPrefix(dsn, "file:")
}

func Skipped(cfg *config.Config) []string {
	var skipped []string
	if cfg.Build != nil && cfg.Build.ProfilesDir != "" {
		skipped = append(skipped, "подготовленные сборки: "+cfg.Build.ProfilesDir)
	}
	if cfg.Storage != nil {
		skipped = append(skipped, "файлы опубликованных сборок в хранилище")
	}
	if cfg.Launcher != nil && cfg.Launcher.Dir != "" {
		skipped = append(skipped, "собранные лаунчеры: "+cfg.Launcher.Dir)
	}
	return skipped
}

func Create(cfg *config.Config, configPath, target string) (*Manifest, error) {
	file, err := os.Create(target)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	compressor := gzip.NewWriter(file)
	archive := tar.NewWriter(compressor)

	manifest := &Manifest{Version: version.Current, CreatedAt: time.Now(), Skipped: Skipped(cfg)}
	seen := map[string]bool{}

	for index, entry := range sources(cfg, configPath) {
		absolute, err := filepath.Abs(entry.path)
		if err != nil || seen[absolute] {
			continue
		}
		info, err := os.Stat(absolute)
		if err != nil || info.IsDir() {
			continue
		}
		seen[absolute] = true

		name := fmt.Sprintf("%s%02d-%s", filesPrefix, index, filepath.Base(absolute))
		if err := write(archive, name, absolute, info); err != nil {
			return nil, err
		}
		manifest.Items = append(manifest.Items, Item{
			Name: name,
			Path: absolute,
			What: entry.what,
			Mode: uint32(info.Mode().Perm()),
		})
	}

	if len(manifest.Items) == 0 {
		return nil, fmt.Errorf("сохранять нечего: ни одного файла из настроек не нашлось на диске")
	}

	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := archive.WriteHeader(&tar.Header{Name: manifestName, Mode: 0o600, Size: int64(len(body))}); err != nil {
		return nil, err
	}
	if _, err := archive.Write(body); err != nil {
		return nil, err
	}

	if err := archive.Close(); err != nil {
		return nil, err
	}
	if err := compressor.Close(); err != nil {
		return nil, err
	}
	return manifest, nil
}

func write(archive *tar.Writer, name, path string, info os.FileInfo) error {
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()

	header := &tar.Header{
		Name:    name,
		Mode:    int64(info.Mode().Perm()),
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}
	if err := archive.WriteHeader(header); err != nil {
		return err
	}
	_, err = io.Copy(archive, source)
	return err
}

func Read(path string) (*Manifest, error) {
	manifest, _, closer, err := open(path)
	if closer != nil {
		defer closer()
	}
	return manifest, err
}

func open(path string) (*Manifest, map[string][]byte, func(), error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, err
	}
	closer := func() { file.Close() }

	compressor, err := gzip.NewReader(file)
	if err != nil {
		return nil, nil, closer, fmt.Errorf("%s не похож на архив сохранения: %w", path, err)
	}

	archive := tar.NewReader(compressor)
	payload := map[string][]byte{}
	var manifest *Manifest

	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, closer, err
		}
		if header.Size > maxFileBytes {
			return nil, nil, closer, fmt.Errorf("файл %s в архиве слишком велик", header.Name)
		}
		body, err := io.ReadAll(io.LimitReader(archive, maxFileBytes))
		if err != nil {
			return nil, nil, closer, err
		}
		if header.Name == manifestName {
			manifest = &Manifest{}
			if err := json.Unmarshal(body, manifest); err != nil {
				return nil, nil, closer, err
			}
			continue
		}
		payload[header.Name] = body
	}

	if manifest == nil {
		return nil, nil, closer, fmt.Errorf("в архиве нет %s — это не сохранение Laminara", manifestName)
	}
	return manifest, payload, closer, nil
}

type Restored struct {
	Item    Item
	Written bool
	Reason  string
}

func Restore(path, into string, force bool) ([]Restored, error) {
	manifest, payload, closer, err := open(path)
	if closer != nil {
		defer closer()
	}
	if err != nil {
		return nil, err
	}

	items := append([]Item{}, manifest.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })

	var results []Restored
	for _, item := range items {
		body, ok := payload[item.Name]
		if !ok {
			results = append(results, Restored{Item: item, Reason: "файла нет в архиве"})
			continue
		}

		target := item.Path
		if into != "" {
			target = filepath.Join(into, strings.TrimPrefix(filepath.ToSlash(item.Path), "/"))
		}
		if _, err := os.Stat(target); err == nil && !force {
			results = append(results, Restored{Item: item, Reason: "уже есть на диске"})
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return results, err
		}
		mode := os.FileMode(item.Mode)
		if mode == 0 {
			mode = 0o600
		}
		if err := os.WriteFile(target, body, mode); err != nil {
			return results, err
		}
		results = append(results, Restored{Item: Item{Name: item.Name, Path: target, What: item.What, Mode: item.Mode}, Written: true})
	}
	return results, nil
}

func DefaultName(now time.Time) string {
	return fmt.Sprintf("laminara-%s.tar.gz", now.Format("20060102-1504"))
}
