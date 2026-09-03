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

func RuntimeDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "laminara")
	}
	if os.Geteuid() == 0 {
		return "/run/laminara"
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
