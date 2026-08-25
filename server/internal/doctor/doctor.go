package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/humanize"
	"github.com/laminara/laminara/server/internal/signing"
	"github.com/laminara/laminara/server/internal/storage"
)

type Verdict int

const (
	OK Verdict = iota
	Warn
	Fail
)

const lowDiskBytes = 5 << 30

type Result struct {
	What    string
	Verdict Verdict
	Detail  string
}

func Run(ctx context.Context, cfg *config.Config, configPath string) []Result {
	var results []Result
	add := func(what string, verdict Verdict, format string, args ...any) {
		results = append(results, Result{What: what, Verdict: verdict, Detail: fmt.Sprintf(format, args...)})
	}

	add("настройки", OK, "%s прочитан", configPath)

	checkSigning(cfg, add)
	checkProfiles(cfg, add)
	checkStorage(ctx, cfg, add)
	checkListener(cfg, add)
	checkYggdrasil(cfg, add)
	checkMachines(cfg, add)
	checkLog(cfg, add)

	return results
}

type reporter func(what string, verdict Verdict, format string, args ...any)

func checkSigning(cfg *config.Config, add reporter) {
	if cfg.Build == nil || cfg.Build.SigningKeyPath == "" {
		add("ключ подписи", Fail, "не задан build.signingKeyPath — публиковать сборки нечем")
		return
	}
	if _, err := os.Stat(cfg.Build.SigningKeyPath); err != nil {
		add("ключ подписи", Warn, "%s ещё не создан — появится при первом запуске", cfg.Build.SigningKeyPath)
		return
	}
	ring, err := signing.NewKeyring(cfg.Build.SigningKeyPath, cfg.Build.TrustedSigningKeys)
	if err != nil {
		add("ключ подписи", Fail, "%v", err)
		return
	}
	add("ключ подписи", OK, "активный %s…, доверенных ключей %d", ring.ActiveHex()[:16], len(ring.TrustedHex()))

	if info, err := os.Stat(cfg.Build.SigningKeyPath); err == nil && info.Mode().Perm()&0o077 != 0 {
		add("права на ключ", Warn, "%s открыт остальным (%v) — оставьте 0600", cfg.Build.SigningKeyPath, info.Mode().Perm())
	}
}

func checkProfiles(cfg *config.Config, add reporter) {
	if cfg.Build == nil || cfg.Build.ProfilesDir == "" {
		add("папка сборок", Warn, "не задана build.profilesDir — готовить сборки будет негде")
		return
	}
	dir := cfg.Build.ProfilesDir
	if err := writable(dir); err != nil {
		add("папка сборок", Fail, "%s: %v", dir, err)
		return
	}
	free, err := freeBytes(dir)
	if err != nil {
		add("папка сборок", OK, "%s доступна на запись", dir)
		return
	}
	verdict := OK
	if free < lowDiskBytes {
		verdict = Warn
	}
	add("папка сборок", verdict, "%s, свободно %s", dir, humanize.Bytes(free))
}

func checkStorage(ctx context.Context, cfg *config.Config, add reporter) {
	if cfg.Storage == nil || cfg.Storage.Backend == "" {
		add("хранилище", Fail, "не задан storage.backend — раздавать файлы нечем")
		return
	}
	backend, err := storage.BuildBackend(cfg.Storage.Backend, cfg.Storage.Config)
	if err != nil {
		add("хранилище", Fail, "%s: %v", cfg.Storage.Backend, err)
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, _, err := backend.Stat(probeCtx, "doctor/probe"); err != nil {
		add("хранилище", Fail, "%s не отвечает: %v", cfg.Storage.Backend, err)
		return
	}
	add("хранилище", OK, "%s отвечает", cfg.Storage.Backend)
}

func checkListener(cfg *config.Config, add reporter) {
	if cfg.API == nil || cfg.API.Addr == "" {
		add("публичный слушатель", Fail, "не задан api.addr — лаунчеру некуда обращаться")
		return
	}
	addr := cfg.API.Addr
	listener, err := net.Listen("tcp", addr)
	if err == nil {
		listener.Close()
		add("публичный слушатель", OK, "%s свободен — сервер сейчас не запущен", addr)
		return
	}
	conn, dialErr := net.DialTimeout("tcp", dialable(addr), 2*time.Second)
	if dialErr == nil {
		conn.Close()
		add("публичный слушатель", OK, "%s занят — похоже, сервер уже работает", addr)
		return
	}
	add("публичный слушатель", Fail, "%s не занять и не достучаться: %v", addr, err)
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

func checkYggdrasil(cfg *config.Config, add reporter) {
	if cfg.Yggdrasil == nil || !cfg.Yggdrasil.Enabled {
		add("вход в игре", Fail, "yggdrasil выключен — войти в игру не сможет никто")
		return
	}
	if cfg.Yggdrasil.RSAKeyPath == "" {
		add("вход в игре", Warn, "не задан yggdrasil.rsaKeyPath — ключ будет создаваться заново при каждом запуске")
		return
	}
	if _, err := os.Stat(cfg.Yggdrasil.RSAKeyPath); err != nil {
		add("вход в игре", Warn, "%s ещё не создан — появится при первом запуске", cfg.Yggdrasil.RSAKeyPath)
		return
	}
	add("вход в игре", OK, "ключ %s на месте", cfg.Yggdrasil.RSAKeyPath)
}

func checkMachines(cfg *config.Config, add reporter) {
	if cfg.HWID == nil {
		add("распознавание компьютеров", Warn, "блока hwid нет — баны по железу работать не будут")
		return
	}
	if cfg.HWID.SaltPath == "" {
		add("соль отпечатков", Warn, "не задан hwid.saltPath — соль будет новой при каждом запуске, а с ней и все компьютеры")
	} else if _, err := os.Stat(cfg.HWID.SaltPath); err != nil {
		add("соль отпечатков", Warn, "%s ещё не создан — появится при первом запуске", cfg.HWID.SaltPath)
	} else {
		add("соль отпечатков", OK, "%s на месте", cfg.HWID.SaltPath)
	}

	var store struct {
		Driver string `json:"driver"`
		DSN    string `json:"dsn"`
	}
	if len(cfg.HWID.Store.Config) > 0 {
		_ = json.Unmarshal(cfg.HWID.Store.Config, &store)
	}
	if cfg.HWID.Store.Backend == "memory" || cfg.HWID.Store.Backend == "" {
		add("база компьютеров", Warn, "хранится в памяти — после перезапуска все машины и баны забудутся")
		return
	}
	add("база компьютеров", OK, "%s", cfg.HWID.Store.Backend)
}

func checkLog(cfg *config.Config, add reporter) {
	if cfg.Log == nil || cfg.Log.File == "" {
		add("журнал", OK, "пишется в консоль — файл ведёт systemd или docker")
		return
	}
	if err := writable(filepath.Dir(cfg.Log.File)); err != nil {
		add("журнал", Fail, "%s: %v", cfg.Log.File, err)
		return
	}
	add("журнал", OK, "%s", cfg.Log.File)
}

func writable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
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

func Worst(results []Result) Verdict {
	worst := OK
	for _, result := range results {
		if result.Verdict > worst {
			worst = result.Verdict
		}
	}
	return worst
}

func Format(results []Result) string {
	var out strings.Builder
	for _, result := range results {
		mark := "  ok  "
		switch result.Verdict {
		case Warn:
			mark = " важно"
		case Fail:
			mark = " плохо"
		}
		fmt.Fprintf(&out, "[%s] %-26s %s\n", mark, result.What, result.Detail)
	}
	return out.String()
}
