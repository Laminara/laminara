package buildspec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"unicode"

	"github.com/laminara/laminara/server/internal/loader"
	"github.com/laminara/laminara/server/internal/platform"
)

const MaxFileSize = 512 << 20
const MaxSpecSize = 48 << 10

type File struct {
	Path   string `json:"path"`
	Source string `json:"source,omitempty"`
	SHA256 string `json:"sha256"`
	Size   uint64 `json:"size"`
}

type Spec struct {
	Version         int                        `json:"version"`
	Name            string                     `json:"name"`
	Minecraft       string                     `json:"minecraft"`
	Loader          string                     `json:"loader"`
	LoaderVersion   string                     `json:"loaderVersion,omitempty"`
	Compat          string                     `json:"compat"`
	Java            string                     `json:"java,omitempty"`
	RuntimeVersions map[string]string          `json:"runtimeVersions,omitempty"`
	Platforms       []string                   `json:"platforms"`
	Settings        map[string]json.RawMessage `json:"settings,omitempty"`
	Files           []File                     `json:"files,omitempty"`
	SourceRevision  string                     `json:"sourceRevision"`
}

func Decode(data []byte) (*Spec, error) {
	if len(data) > MaxSpecSize {
		return nil, errors.New("описание сборки превышает 48 КиБ")
	}
	var spec Spec
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("после описания сборки есть лишние данные")
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return &spec, nil
}

func SafePath(value string) bool {
	if value == "" || strings.ContainsAny(value, "\\:\x00") || path.IsAbs(value) || path.Clean(value) != value {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "." || part == ".." || strings.HasPrefix(part, ".") || strings.TrimSpace(part) != part || strings.ContainsFunc(part, unicode.IsControl) {
			return false
		}
	}
	first, _, _ := strings.Cut(value, "/")
	return !slices.Contains([]string{"platforms", "runtime", "assets", "libraries", "versions"}, first) && !strings.HasPrefix(first, "laminara.")
}

func ValidHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(value) == value
}

func (s *Spec) Validate() error {
	if s.Version != 1 || !SafePath(s.Name) || strings.Contains(s.Name, "/") {
		return errors.New("нужны version=1 и безопасное имя сборки")
	}
	if s.Minecraft == "" || slices.Contains([]string{"release", "snapshot", "latest"}, s.Minecraft) {
		return errors.New("закрепите точную версию Minecraft")
	}
	if s.SourceRevision == "" || len(s.SourceRevision) > 256 || strings.ContainsFunc(s.SourceRevision, unicode.IsControl) {
		return errors.New("нужна sourceRevision длиной до 256 символов")
	}
	if s.Loader != "vanilla" {
		if _, ok := loader.Get(s.Loader); !ok {
			return errors.New("неизвестный loader")
		}
		if s.LoaderVersion == "" || s.LoaderVersion == "latest" {
			return errors.New("закрепите loaderVersion")
		}
	}
	if s.Compat != "off" {
		_, version, ok := strings.Cut(s.Compat, ":")
		if !ok || version == "" || slices.Contains([]string{"latest", "последняя", "новая", "свежая"}, version) {
			return errors.New("compat должен быть off или рецепт:точная-версия")
		}
	}
	if len(s.Platforms) == 0 {
		return errors.New("нужны явные платформы")
	}
	seen := map[string]bool{}
	for _, name := range s.Platforms {
		p, ok := platform.Parse(name)
		if !ok {
			return fmt.Errorf("неизвестная платформа %s", name)
		}
		canonical, _ := platform.Key(p)
		if name != canonical || seen[name] {
			return errors.New("платформы должны иметь уникальные канонические имена")
		}
		seen[name] = true
	}
	allowed := []string{"serverAddress", "userWritable", "enforced", "jvmArgs", "gameArgs", "classpath", "classpathExclude", "mainClass", "features"}
	for target, version := range s.RuntimeVersions {
		if !seen[target] || strings.TrimSpace(version) == "" || version == "latest" {
			return errors.New("runtimeVersions задаёт точную версию Java для выбранной платформы")
		}
	}
	for key := range s.Settings {
		if !slices.Contains(allowed, key) {
			return fmt.Errorf("поле settings.%s не является пользовательской настройкой", key)
		}
	}
	seen = map[string]bool{}
	var total uint64
	if len(s.Files) > 200 {
		return errors.New("допускается до 200 файлов")
	}
	for _, file := range s.Files {
		key := strings.ToLower(file.Path)
		if !SafePath(file.Path) || seen[key] || file.Size > MaxFileSize || (file.Source == "" && !ValidHash(file.SHA256)) {
			return fmt.Errorf("неверный файл %q", file.Path)
		}
		for previous := range seen {
			if strings.HasPrefix(key, previous+"/") || strings.HasPrefix(previous, key+"/") {
				return errors.New("файл перекрывает каталог другого файла")
			}
		}
		seen[key] = true
		total += file.Size
	}
	if total > 8<<30 {
		return errors.New("файлы сборки превышают 8 ГиБ")
	}
	return nil
}

func (s *Spec) Canonical() ([]byte, string, error) {
	copy := *s
	copy.Platforms = slices.Clone(s.Platforms)
	slices.Sort(copy.Platforms)
	copy.Files = slices.Clone(s.Files)
	for i := range copy.Files {
		copy.Files[i].Source = ""
	}
	slices.SortFunc(copy.Files, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	data, err := json.Marshal(copy)
	if err != nil {
		return nil, "", err
	}
	return data, fmt.Sprintf("%x", sha256.Sum256(data)), nil
}
