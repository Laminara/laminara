package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/laminara/laminara/server/internal/config"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestServerFillsEveryPathItNeeds(t *testing.T) {
	path := writeConfig(t, `{
		"auth": {"provider": "jsonfile", "config": {"path": "/var/lib/laminara/users.json"}},
		"storage": {"backend": "fs", "config": {"root": "/var/lib/laminara/objects"}},
		"build": {"profilesDir": "/var/lib/laminara/profiles"},
		"yggdrasil": {"enabled": true},
		"hwid": {"mode": "observe"},
		"console": {"enabled": true}
	}`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	filled := map[string]string{
		"build.signingKeyPath":  cfg.Build.SigningKeyPath,
		"hwid.saltPath":         cfg.HWID.SaltPath,
		"hwid.ticketSecretPath": cfg.HWID.TicketSecretPath,
		"hwid.store.backend":    cfg.HWID.Store.Backend,
		"yggdrasil.rsaKeyPath":  cfg.Yggdrasil.RSAKeyPath,
		"console.statePath":     cfg.Console.StatePath,
		"modules.dir":           cfg.Modules.Dir,
	}
	for name, value := range filled {
		if value == "" {
			t.Errorf("%s остался пустым — оператор напишет конфиг без него и наткнётся на отказ там, где сервер мог вывести путь сам", name)
		}
	}
	if got := filepath.Dir(cfg.HWID.SaltPath); got != "/var/lib/laminara" {
		t.Errorf("соль легла не в каталог данных, а в %s", got)
	}
	if cfg.HWID.Store.Backend != "sql" {
		t.Errorf("база компьютеров по умолчанию %q — при memory баны по железу исчезают при каждом перезапуске", cfg.HWID.Store.Backend)
	}
}

func TestOperatorChoiceWinsOverDefault(t *testing.T) {
	path := writeConfig(t, `{
		"build": {"profilesDir": "/srv/p", "signingKeyPath": "/keys/signing.key"},
		"yggdrasil": {"enabled": true, "rsaKeyPath": "/keys/ygg.pem"},
		"hwid": {"mode": "enforce", "saltPath": "/keys/salt", "store": {"backend": "memory"}},
		"console": {"statePath": "/state/console.json"},
		"modules": {"dir": "/opt/modules"}
	}`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, pair := range map[string][2]string{
		"hwid.saltPath":        {cfg.HWID.SaltPath, "/keys/salt"},
		"hwid.store.backend":   {cfg.HWID.Store.Backend, "memory"},
		"yggdrasil.rsaKeyPath": {cfg.Yggdrasil.RSAKeyPath, "/keys/ygg.pem"},
		"console.statePath":    {cfg.Console.StatePath, "/state/console.json"},
		"modules.dir":          {cfg.Modules.Dir, "/opt/modules"},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s: умолчание перебило выбор оператора — %q вместо %q", name, pair[0], pair[1])
		}
	}
}

func TestHwidOffNeedsNoSecrets(t *testing.T) {
	path := writeConfig(t, `{
		"build": {"profilesDir": "/var/lib/laminara/profiles"},
		"hwid": {"mode": "off"}
	}`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HWID.SaltPath != "" || cfg.HWID.TicketSecretPath != "" {
		t.Fatal("выключенный hwid не должен заводить себе файлы секретов")
	}
}
