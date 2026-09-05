package control

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	socketName = "control.sock"
	pidName    = "laminara.pid"
)

const systemRuntimeDir = "/run/laminara"

func RuntimeDir() string {
	if dir := os.Getenv("LAMINARA_RUNTIME_DIR"); dir != "" {
		return dir
	}
	// Системная установка (демон под systemd или root) держит сокет здесь.
	// Пока каталог есть, и демон, и клиент под любым пользователем сходятся в
	// одной точке — иначе console/status ищут сокет по своему XDG_RUNTIME_DIR
	// и не находят демона.
	if info, err := os.Stat(systemRuntimeDir); err == nil && info.IsDir() {
		return systemRuntimeDir
	}
	if os.Geteuid() == 0 {
		return systemRuntimeDir
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "laminara")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("laminara-%d", os.Getuid()))
}

func SocketPath() string { return filepath.Join(RuntimeDir(), socketName) }

func PidPath() string { return filepath.Join(RuntimeDir(), pidName) }

func Listen() (net.Listener, error) {
	path := SocketPath()
	if conn, err := net.Dial("unix", path); err == nil {
		conn.Close()
		return nil, fmt.Errorf("laminara-server already running at %s", path)
	}
	if err := os.MkdirAll(RuntimeDir(), 0o700); err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

func ReleasePid() {
	raw, err := os.ReadFile(PidPath())
	if err != nil {
		return
	}
	if strings.TrimSpace(string(raw)) != strconv.Itoa(os.Getpid()) {
		return
	}
	_ = os.Remove(PidPath())
}
