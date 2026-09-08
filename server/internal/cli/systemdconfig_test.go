package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/hwid"
	"github.com/laminara/laminara/server/internal/webconsole"
)

func TestUnitGivesModulesAWritableTempDir(t *testing.T) {
	unit := renderSystemd(systemdOptions{Binary: "/usr/local/bin/laminara-server", Config: "/etc/laminara/config.json", User: "laminara", Group: "laminara"})
	if strings.Contains(unit, "ProtectSystem=strict") && !strings.Contains(unit, "PrivateTmp=true") {
		t.Fatal("ProtectSystem=strict без PrivateTmp делает /tmp только для чтения, и ни один модуль не поднимется: go-plugin кладёт туда свой сокет")
	}
}

func TestUnitRestartsOnFailureButNotOnBadConfig(t *testing.T) {
	unit := renderSystemd(systemdOptions{Binary: "/usr/local/bin/laminara-server", Config: "/etc/laminara/config.json", User: "laminara", Group: "laminara"})
	for _, want := range []string{"Restart=on-failure", "RestartPreventExitStatus=78", "Type=notify", "Environment=XDG_RUNTIME_DIR=/run/laminara"} {
		if !strings.Contains(unit, want) {
			t.Fatalf("в unit нет %q:\n%s", want, unit)
		}
	}
}

func TestWritablePathsCoverEveryPlaceTheServerWrites(t *testing.T) {
	install := true
	cfg := &config.Config{
		Build:     &config.BuildConfig{ProfilesDir: "/srv/сборки/profiles", SigningKeyPath: "/srv/ключи/signing.key"},
		Storage:   &config.StorageConfig{Backend: "fs", Config: json.RawMessage(`{"root":"/mnt/объекты"}`)},
		Launcher:  &config.LauncherConfig{Dir: "/srv/лаунчер"},
		Yggdrasil: &config.YggdrasilConfig{RSAKeyPath: "/srv/ключи/ygg.pem"},
		HWID:      &hwid.Config{SaltPath: "/srv/ключи/hwid.salt", Store: hwid.StoreConfig{Backend: "sql", Config: json.RawMessage(`{"dsn":"file:/srv/база/hwid.db?_pragma=busy_timeout(5000)"}`)}},
		Console:   &webconsole.Config{StatePath: "/var/lib/laminara/console.json"},
		Log:       &config.LogConfig{File: "/var/log/laminara/server.log"},
		Update:    &config.UpdateConfig{Install: install},
	}

	paths := writablePaths(cfg, "/etc/laminara/config.json", "/usr/local/bin/laminara-server")
	for _, want := range []string{"/srv/сборки/profiles", "/srv/ключи", "/mnt/объекты", "/srv/лаунчер", "/srv/база", "/var/log/laminara", "/etc/laminara", "/run/laminara", "/usr/local/bin"} {
		if !hasPath(paths, want) {
			t.Fatalf("сервер пишет в %s, но в ReadWritePaths его нет: %v", want, paths)
		}
	}
}

func TestWritablePathsDropNestedAndRelative(t *testing.T) {
	cfg := &config.Config{
		Build:    &config.BuildConfig{ProfilesDir: "/var/lib/laminara/profiles", SigningKeyPath: "/var/lib/laminara/signing.key"},
		Storage:  &config.StorageConfig{Backend: "fs", Config: json.RawMessage(`{"root":"objects"}`)},
		Launcher: &config.LauncherConfig{Dir: "/var/lib/laminara/launcher"},
	}
	paths := writablePaths(cfg, "/var/lib/laminara/config.json", "/usr/local/bin/laminara-server")
	if len(paths) != 2 || !hasPath(paths, "/var/lib/laminara") || !hasPath(paths, "/run/laminara") {
		t.Fatalf("вложенные пути не схлопнулись, а относительный не отброшен: %v", paths)
	}
}

func hasPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
