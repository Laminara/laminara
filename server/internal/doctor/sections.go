package doctor

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/diag"
	"github.com/laminara/laminara/server/internal/humanize"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/signing"
	"github.com/laminara/laminara/server/internal/storage"
	"github.com/laminara/laminara/server/internal/tempdir"
)

const lowDiskBytes = 5 << 30

func inspect(ctx context.Context, probe *diag.Probe, component any) bool {
	checker, ok := component.(diag.Checker)
	if !ok {
		return false
	}
	checker.Check(ctx, probe)
	return true
}

func checkConfigFile(_ context.Context, opts Options, probe *diag.Probe) {
	if opts.ConfigPath == "" {
		probe.Skip("файл настроек", "проверка идёт у запущенного сервера")
	} else {
		probe.OK("файл настроек", "%s прочитан", opts.ConfigPath)
		checkConfigPermissions(opts.ConfigPath, probe)
		checkUnknownSections(opts.ConfigPath, probe)
	}
	reportBuildError(opts, probe)
}

func reportBuildError(opts Options, probe *diag.Probe) {
	if opts.BuildError == nil {
		return
	}
	probe.Fail("сборка сервера", fmt.Sprintf("с этими настройками сервер не поднимется: %v", opts.BuildError), diag.Remedy{
		Hint: "пока это не починено, до самих компонентов дело не доходит и почти все проверки ниже пропускаются",
	})
}

func checkConfigPermissions(path string, probe *diag.Probe) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.Mode().Perm()&0o077 == 0 {
		return
	}
	probe.Warn("права на настройки", fmt.Sprintf("%s открыт остальным (%v) — в нём пароли к базе и ключам", path, info.Mode().Perm()), diag.Remedy{
		Hint:    "оставьте 0600",
		Command: fmt.Sprintf("chmod 600 %s", path),
		Apply: func(context.Context) error {
			return os.Chmod(path, 0o600)
		},
	})
}

func checkUnknownSections(path string, probe *diag.Probe) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	known := map[string]bool{}
	fields := reflect.TypeOf(config.Config{})
	for i := 0; i < fields.NumField(); i++ {
		tag := fields.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name != "" && name != "-" {
			known[name] = true
		}
	}
	var unknown []string
	for name := range raw {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return
	}
	sort.Strings(unknown)
	probe.Warn("незнакомые разделы", fmt.Sprintf("сервер не знает про %s и молча их игнорирует", strings.Join(unknown, ", ")), diag.Remedy{
		Hint: fmt.Sprintf("чаще всего это опечатка в названии раздела; известные: %s", strings.Join(sortedKeys(known), ", ")),
	})
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func checkAuth(ctx context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.Auth == nil || cfg.Auth.Provider == "" {
		probe.Fail("провайдер", "раздел auth не заполнен — войти не сможет никто", diag.Remedy{
			Hint:    fmt.Sprintf("укажите источник аккаунтов, доступны: %s", strings.Join(auth.ProviderNames(), ", ")),
			Command: "laminara-server settings auth.provider sql",
		})
		return
	}
	known := false
	for _, name := range auth.ProviderNames() {
		if name == cfg.Auth.Provider {
			known = true
			break
		}
	}
	if !known {
		probe.Fail("провайдер", fmt.Sprintf("%q не зарегистрирован", cfg.Auth.Provider), diag.Remedy{
			Hint: fmt.Sprintf("доступны: %s; имя от модуля появляется только после его загрузки", strings.Join(auth.ProviderNames(), ", ")),
		})
		return
	}
	if opts.Wired == nil || opts.Wired.Auth == nil {
		probe.Skip("провайдер", "%s указан, но компоненты не собраны", cfg.Auth.Provider)
		return
	}
	probe.OK("провайдер", "%s", cfg.Auth.Provider)

	service := opts.Wired.Auth
	if !inspect(ctx, probe, service.Provider()) {
		probe.Skip("источник аккаунтов", "провайдер %s не умеет проверять себя", cfg.Auth.Provider)
	}
	checkRejection(ctx, probe, service)

	if !inspect(ctx, probe, service.Sessions()) {
		probe.Warn("сессии", "хранятся в памяти", diag.Remedy{
			Hint:    "после перезапуска сервера всех игроков выкинет из лаунчера, и им придётся входить заново",
			Command: "laminara-server settings auth.sessions.backend redis",
		})
	}
	checkLifetimes(probe, service)
}

func checkRejection(ctx context.Context, probe *diag.Probe, service *auth.Service) {
	callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := service.Verify(callCtx, "laminara-doctor-"+uuid.NewString(), uuid.NewString(), "")
	switch {
	case err == nil:
		probe.Fail("отказ чужому", "несуществующий аккаунт со случайным паролем пустили внутрь", diag.Remedy{
			Hint: "источник аккаунтов отвечает успехом на любой запрос — войти сможет кто угодно под любым ником, включая администраторов",
		})
	case isRejection(err):
		probe.OK("отказ чужому", "чужой пароль отклоняется")
	default:
		probe.Warn("отказ чужому", fmt.Sprintf("проверка не дошла до ответа: %v", err), diag.Remedy{
			Hint: "источник аккаунтов отвечает ошибкой вместо отказа — игроки увидят «неверный пароль» даже с верным",
		})
	}
}

func isRejection(err error) bool {
	return err != nil && strings.Contains(err.Error(), auth.ErrInvalidCredentials.Error())
}

func checkLifetimes(probe *diag.Probe, service *auth.Service) {
	access, refresh := service.Lifetimes()
	if refresh <= access {
		probe.Warn("сроки токенов", fmt.Sprintf("refreshTTL (%s) не больше accessTTL (%s)", humanize.Duration(refresh), humanize.Duration(access)), diag.Remedy{
			Hint:    "игрока будет выкидывать из лаунчера, как только истечёт короткий токен",
			Command: "laminara-server settings auth.refreshTTL 720h",
		})
		return
	}
	if access > 24*time.Hour {
		probe.Warn("сроки токенов", fmt.Sprintf("accessTTL %s — это долго", humanize.Duration(access)), diag.Remedy{
			Hint: "украденный токен будет годен всё это время; разумный порядок — минуты или часы",
		})
		return
	}
	probe.OK("сроки токенов", "вход на %s, продление до %s", humanize.Duration(access), humanize.Duration(refresh))
}

func checkStorage(ctx context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.Storage == nil || cfg.Storage.Backend == "" {
		probe.Fail("хранилище", "раздел storage не заполнен — раздавать файлы сборок нечем", diag.Remedy{
			Hint:    fmt.Sprintf("доступны: %s", strings.Join(storage.BackendNames(), ", ")),
			Command: "laminara-server settings storage.backend fs",
		})
		return
	}
	if opts.Wired == nil || opts.Wired.Storage == nil {
		probe.Skip("хранилище", "%s указано, но компоненты не собраны", cfg.Storage.Backend)
		return
	}
	probe.OK("бэкенд", "%s", cfg.Storage.Backend)
	if !inspect(ctx, probe, opts.Wired.Storage) {
		probe.Skip("заливка", "бэкенд %s не умеет проверять себя", cfg.Storage.Backend)
	}
	if cfg.Storage.Backend == "fs" {
		checkFreeSpace(probe, storageRoot(cfg), "каталог объектов")
	}
	checkScratch(opts, probe)
}

func checkScratch(opts Options, probe *diag.Probe) {
	system := tempdir.System()
	if err := tempdir.Writable(system); err == nil {
		probe.OK("временные файлы", "%s", system)
		return
	}
	spare, err := tempdir.Fallback(tempdir.Candidates(opts.Config, opts.ConfigPath)...)
	if err != nil {
		probe.Fail("временные файлы", fmt.Sprintf("%s только для чтения, запасного каталога тоже нет", system), diag.Remedy{
			Hint:    "публикация, сборка лаунчера и модули пишут временные файлы; без них сервер работать не будет",
			Command: "laminara-server systemd-config --config " + opts.ConfigPath + " | sudo tee /etc/systemd/system/laminara-server.service && sudo systemctl daemon-reload && sudo systemctl restart laminara-server",
		})
		return
	}
	probe.Warn("временные файлы", fmt.Sprintf("%s только для чтения, сервер пишет в %s", system, spare), diag.Remedy{
		Hint:    "работать будет, но правильнее вернуть системный каталог: в unit systemd нужен PrivateTmp=true",
		Command: "laminara-server systemd-config --config " + opts.ConfigPath + " | sudo tee /etc/systemd/system/laminara-server.service && sudo systemctl daemon-reload && sudo systemctl restart laminara-server",
	})
}

func storageRoot(cfg *config.Config) string {
	if cfg.Storage == nil {
		return ""
	}
	var fs struct {
		Root string `json:"root"`
	}
	_ = json.Unmarshal(cfg.Storage.Config, &fs)
	return fs.Root
}

func checkFreeSpace(probe *diag.Probe, dir, what string) {
	if dir == "" {
		return
	}
	free, err := freeBytes(dir)
	if err != nil {
		return
	}
	if free < lowDiskBytes {
		probe.Warn("место на диске", fmt.Sprintf("%s: свободно %s", what, humanize.Bytes(free)), diag.Remedy{
			Hint: "сборки занимают гигабайты; когда место кончится, публикация оборвётся на середине",
		})
		return
	}
	probe.OK("место на диске", "%s: свободно %s", what, humanize.Bytes(free))
}

func checkBuild(ctx context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.Build == nil || cfg.Build.ProfilesDir == "" {
		probe.Fail("папка сборок", "не задан build.profilesDir — готовить сборки будет негде", diag.Remedy{
			Command: "laminara-server settings build.profilesDir /var/lib/laminara/profiles",
			Hint:    "это каталог, в котором лежат подготовленные клиенты",
		})
		return
	}
	dir := cfg.Build.ProfilesDir
	if err := writable(dir); err != nil {
		probe.Fail("папка сборок", fmt.Sprintf("%s: %v", dir, err), diag.Remedy{
			Hint:    "каталог должен принадлежать пользователю, под которым работает сервер",
			Command: fmt.Sprintf("mkdir -p %s", dir),
			Apply: func(context.Context) error {
				return os.MkdirAll(dir, 0o750)
			},
		})
		return
	}
	probe.OK("папка сборок", "%s доступна на запись", dir)
	checkFreeSpace(probe, dir, "папка сборок")
	checkSigning(ctx, opts, probe)
	checkPublished(ctx, opts, probe)
}

func checkSigning(_ context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.Build.SigningKeyPath == "" {
		probe.Fail("ключ подписи", "не задан build.signingKeyPath — публиковать сборки нечем", diag.Remedy{
			Hint:    "без постоянного ключа он создаётся заново при каждом запуске, и старые лаунчеры перестают принимать манифесты",
			Command: "laminara-server settings build.signingKeyPath /var/lib/laminara/signing.key",
		})
		return
	}
	path := cfg.Build.SigningKeyPath
	info, err := os.Stat(path)
	if err != nil {
		probe.Warn("ключ подписи", fmt.Sprintf("%s ещё не создан — появится при первом запуске", path), diag.Remedy{
			Hint: "после первого запуска сохраните его в резервную копию: потеряв ключ, вы не сможете переподписать сборки для уже установленных лаунчеров",
		})
		return
	}
	ring := opts.Wired.Signing
	if ring == nil {
		built, err := signing.NewKeyring(path, cfg.Build.TrustedSigningKeys)
		if err != nil {
			probe.Fail("ключ подписи", fmt.Sprintf("%v", err), diag.Remedy{
				Hint: "файл ключа повреждён или это не ключ Laminara",
			})
			return
		}
		ring = built
	}
	probe.OK("ключ подписи", "активный %s…, дополнительно доверенных %d", ring.ActiveHex()[:16], len(ring.TrustedHex())-1)

	if info.Mode().Perm()&0o077 != 0 {
		probe.Warn("права на ключ", fmt.Sprintf("%s открыт остальным (%v)", path, info.Mode().Perm()), diag.Remedy{
			Hint:    "с этим ключом можно подписать любую сборку для ваших игроков",
			Command: fmt.Sprintf("chmod 600 %s", path),
			Apply: func(context.Context) error {
				return os.Chmod(path, 0o600)
			},
		})
	}
	checkSelfSign(probe, ring)
}

func checkSelfSign(probe *diag.Probe, ring *signing.Keyring) {
	key := ring.Active()
	public, ok := key.Public().(ed25519.PublicKey)
	if !ok {
		probe.Fail("подпись манифеста", "активный ключ не ed25519", diag.Remedy{
			Hint: "файл ключа повреждён — восстановите его из резервной копии",
		})
		return
	}
	canonical, signature, err := manifest.NewSigner(key).Sign(&corev1.Manifest{Modpack: "laminara-doctor"})
	if err != nil {
		probe.Fail("подпись манифеста", fmt.Sprintf("подписать не удалось: %v", err), diag.Remedy{
			Hint: "ключ есть, но подпись им не считается — файл ключа повреждён",
		})
		return
	}
	if !manifest.Verify(public, canonical, signature) {
		probe.Fail("подпись манифеста", "собственная подпись не проходит проверку", diag.Remedy{
			Hint: "лаунчеры отвергнут любую опубликованную сборку; восстановите ключ из резервной копии",
		})
		return
	}
	if !ring.Trusts(public) {
		probe.Fail("кольцо ключей", "активный ключ не входит в кольцо доверия", diag.Remedy{
			Hint: "лаунчер принимает манифесты только от ключей из кольца; добавьте активный ключ в build.trustedSigningKeys",
		})
		return
	}
	probe.OK("подпись манифеста", "подпись ставится и проверяется")
}

func writable(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	probe, err := os.CreateTemp(dir, ".doctor-*")
	if err != nil {
		return fmt.Errorf("нет прав на запись")
	}
	name := probe.Name()
	probe.Close()
	return os.Remove(name)
}

func freeBytes(dir string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

func checkLog(_ context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.Log == nil || cfg.Log.File == "" {
		probe.OK("журнал", "пишется в консоль — файл ведёт systemd или docker")
		return
	}
	dir := filepath.Dir(cfg.Log.File)
	if err := writable(dir); err != nil {
		probe.Fail("журнал", fmt.Sprintf("%s: %v", cfg.Log.File, err), diag.Remedy{
			Hint:    "каталог журнала должен быть доступен серверу на запись",
			Command: fmt.Sprintf("mkdir -p %s", dir),
			Apply: func(context.Context) error {
				return os.MkdirAll(dir, 0o750)
			},
		})
		return
	}
	probe.OK("журнал", "%s, хранится %d файлов", cfg.Log.File, cfg.Log.Keep)
	checkFreeSpace(probe, dir, "каталог журнала")
}

func dialable(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}
