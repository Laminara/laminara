package e2e

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const password = "правильный-пароль"

type server struct {
	t       *testing.T
	binary  string
	root    string
	config  string
	address string
	process *exec.Cmd
	log     *os.File
}

func TestOperatorAndPlayerFlows(t *testing.T) {
	if os.Getenv("LAMINARA_FLOWS") == "" {
		t.Skip("сквозной прогон включается LAMINARA_FLOWS=1")
	}
	s := start(t)
	defer s.stop()

	t.Run("сервер отвечает после запуска", s.serverAnswers)
	t.Run("подпись «не задано» не попадает в настройки", s.placeholderRefused)
	t.Run("верная настройка записывается", s.settingAccepted)
	t.Run("перезапуск проходит один раз", s.restartHappensOnce)
	t.Run("сборка публикуется и отдаётся игроку", s.publishAndServe)
	t.Run("лаунчер публикуется и качается по ссылке", s.launcherServed)
	t.Run("вторая версия лаунчера выпускается", s.secondLauncherVersion)
	t.Run("весь путь игрока проходит", s.playerPath)
	t.Run("диагностика не находит поломок", s.doctorIsClean)
	t.Run("испорченный конфиг останавливает перезапуски", s.badConfigStopsRestarts)
}

func start(t *testing.T) *server {
	t.Helper()
	root := t.TempDir()
	binary := filepath.Join(root, "laminara-server")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binary, "../../cmd/laminara-server")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("сервер не собрался: %v\n%s", err, out)
	}

	s := &server{t: t, binary: binary, root: root, address: "127.0.0.1:" + freePort(t)}
	s.config = filepath.Join(root, "config.json")
	s.writeConfig(map[string]any{})
	s.run("hash", "--algo", "argon2id", password)
	hash := strings.TrimSpace(s.run("hash", "--algo", "argon2id", password))
	write(t, filepath.Join(root, "users.json"), fmt.Sprintf(`[{"username":"Steve","password":%q}]`, hash))
	s.launch()
	return s
}

func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	return port
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (s *server) writeConfig(extra map[string]any) {
	s.t.Helper()
	cfg := map[string]any{
		"auth": map[string]any{
			"provider": "jsonfile",
			"config":   map[string]any{"path": filepath.Join(s.root, "users.json"), "hash": "argon2id"},
		},
		"storage": map[string]any{"backend": "fs", "config": map[string]any{"root": filepath.Join(s.root, "objects")}},
		"build": map[string]any{
			"profilesDir":    filepath.Join(s.root, "profiles"),
			"signingKeyPath": filepath.Join(s.root, "signing.key"),
		},
		"launcher": map[string]any{
			"dir":       filepath.Join(s.root, "launcher"),
			"endpoints": []string{"http://" + s.address},
		},
		"api":       map[string]any{"addr": s.address},
		"console":   map[string]any{"enabled": false},
		"update":    map[string]any{"check": false},
		"branding":  map[string]any{"name": "ПРОГОН", "windowTitle": "Прогон"},
		"yggdrasil": map[string]any{"enabled": true, "rsaKeyPath": filepath.Join(s.root, "ygg.pem"), "skinProvider": "template", "skinConfig": map[string]any{"skin": "https://skins.test/%nickname%.png"}, "skinDomains": []string{"skins.test"}},
	}
	for key, value := range extra {
		cfg[key] = value
	}
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		s.t.Fatal(err)
	}
	write(s.t, s.config, string(encoded))
}

func (s *server) launch() {
	s.t.Helper()
	log, err := os.Create(filepath.Join(s.root, "server.log"))
	if err != nil {
		s.t.Fatal(err)
	}
	s.log = log
	s.process = exec.Command(s.binary, "start", "--config", s.config)
	s.process.Stdout = log
	s.process.Stderr = log
	if err := s.process.Start(); err != nil {
		s.t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", s.address, time.Second); err == nil {
			conn.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	s.t.Fatalf("сервер не поднялся за 20 секунд:\n%s", s.logs())
}

func (s *server) stop() {
	_ = exec.Command(s.binary, "stop").Run()
	if s.process != nil && s.process.Process != nil {
		_ = s.process.Process.Kill()
		_ = s.process.Wait()
	}
	if s.log != nil {
		s.log.Close()
	}
}

func (s *server) logs() string {
	data, err := os.ReadFile(filepath.Join(s.root, "server.log"))
	if err != nil {
		return ""
	}
	return string(data)
}

func (s *server) run(args ...string) string {
	s.t.Helper()
	out, _ := exec.Command(s.binary, args...).CombinedOutput()
	return string(out)
}

func (s *server) exec(line string) string {
	s.t.Helper()
	out, _ := exec.Command(s.binary, "exec", line).CombinedOutput()
	return string(out)
}

func (s *server) get(path string) (int, string) {
	s.t.Helper()
	response, err := http.Get("http://" + s.address + path)
	if err != nil {
		s.t.Fatalf("запрос %s не прошёл: %v", path, err)
	}
	defer response.Body.Close()
	body := make([]byte, 1<<20)
	read, _ := response.Body.Read(body)
	return response.StatusCode, string(body[:read])
}

func (s *server) serverAnswers(t *testing.T) {
	if out := s.run("status"); !strings.Contains(out, "в работе") {
		t.Fatalf("сервер не отвечает на status: %s\n%s", out, s.logs())
	}
}

func (s *server) placeholderRefused(t *testing.T) {
	out := s.exec(`settings api.trustedProxies "не задано"`)
	if !strings.Contains(out, "не поднимется") {
		t.Fatalf("подпись «не задано» записалась бы в настройки: %s", out)
	}
	raw, err := os.ReadFile(s.config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "не задано") {
		t.Fatal("подпись попала в файл настроек")
	}
}

func (s *server) settingAccepted(t *testing.T) {
	if out := s.exec("settings api.trustedProxies 127.0.0.1"); !strings.Contains(out, "Записано") {
		t.Fatalf("рабочая настройка не записалась: %s", out)
	}
}

func (s *server) restartHappensOnce(t *testing.T) {
	before := strings.Count(s.logs(), "сервер запущен")
	s.exec("restart")
	time.Sleep(4 * time.Second)
	if out := s.run("status"); !strings.Contains(out, "в работе") {
		t.Fatalf("после перезапуска сервер не поднялся: %s\n%s", out, s.logs())
	}
	if after := strings.Count(s.logs(), "сервер запущен"); after != before+1 {
		t.Fatalf("сервер перезапустился %d раз вместо одного", after-before)
	}
}

func (s *server) publishAndServe(t *testing.T) {
	profile := filepath.Join(s.root, "profiles", "pack", "platforms", "linux")
	write(t, filepath.Join(profile, "laminara.profile.json"), `{
		"mainClass": "net.minecraft.client.main.Main",
		"javaComponent": "jre-legacy", "javaMajor": 8,
		"os": "linux", "arch": "x86_64", "platformKey": "linux",
		"javaBin": "runtime/bin/java", "versionId": "1.20.1", "assetIndex": "5",
		"clientJar": "versions/client.jar", "classpath": ["versions/client.jar"], "runtime": "runtime"
	}`)
	write(t, filepath.Join(s.root, "profiles", "pack", "versions", "client.jar"), "это как бы клиент")
	write(t, filepath.Join(s.root, "profiles", "pack", "mods", "мод.jar"), "это как бы мод")
	write(t, filepath.Join(s.root, "profiles", "pack", "authlib-injector.jar"), "это как бы authlib-injector")
	write(t, filepath.Join(profile, "runtime", "bin", "java"), "это как бы java")

	if out := s.exec("publish pack"); !strings.Contains(out, "Опубликована") {
		t.Fatalf("публикация не прошла: %s", out)
	}
	if code, _ := s.get("/healthz"); code != http.StatusOK {
		t.Fatalf("сервер не отвечает на /healthz: %d", code)
	}
}

func (s *server) launcherServed(t *testing.T) {
	write(t, filepath.Join(s.root, "launcher", "1.0.0", "Прогон.exe"), "это как бы лаунчер")
	write(t, filepath.Join(s.root, "launcher", "1.0.0", "laminara-launcher-linux-x86_64"), "это как бы лаунчер linux")
	if out := s.exec("launcher publish 1.0.0"); !strings.Contains(out, "опубликован") {
		t.Fatalf("лаунчер не опубликовался: %s", out)
	}

	code, body := s.get("/launcher/windows-x64")
	if code != http.StatusOK {
		t.Fatalf("лаунчер не отдаётся по постоянной ссылке: %d", code)
	}
	if body != "это как бы лаунчер" {
		t.Fatalf("по ссылке пришёл не тот файл: %q", body)
	}
	if code, list := s.get("/launcher"); code != http.StatusOK || !strings.Contains(list, "/launcher/linux") {
		t.Fatalf("список систем не отдаётся: %d %s", code, list)
	}
}

func (s *server) secondLauncherVersion(t *testing.T) {
	write(t, filepath.Join(s.root, "launcher", "1.0.1", "Прогон.exe"), "это как бы лаунчер новее")
	if out := s.exec("launcher publish 1.0.1"); !strings.Contains(out, "опубликован") {
		t.Fatalf("вторую версию лаунчера выпустить не дали: %s", out)
	}
	_, body := s.get("/launcher/windows-x64")
	if body != "это как бы лаунчер новее" {
		t.Fatalf("по ссылке остался старый файл: %q", body)
	}
}

func (s *server) playerPath(t *testing.T) {
	out := s.run("doctor", "--config", s.config, "--only", "flow", "--as", "Steve", "--password", password)
	for _, step := range []string{"вход", "продление входа", "защита от кражи токена", "список сборок", "манифест сборки", "скачивание файла", "вход в игре", "вход на сервер игры"} {
		if !strings.Contains(out, step) {
			t.Fatalf("шаг %q не пройден:\n%s", step, out)
		}
	}
	if strings.Contains(out, "плохо") {
		t.Fatalf("путь игрока прерывается:\n%s", out)
	}
}

func (s *server) doctorIsClean(t *testing.T) {
	out := s.run("doctor", "--config", s.config, "--as", "Steve", "--password", password)
	if strings.Contains(out, "плохо") {
		t.Fatalf("диагностика нашла поломку на исправном сервере:\n%s", out)
	}
}

func (s *server) badConfigStopsRestarts(t *testing.T) {
	broken := filepath.Join(s.root, "broken.json")
	write(t, broken, `{"api":{"addr":"127.0.0.1:8099","trustedProxies":["не задано"]}}`)

	command := exec.Command(s.binary, "start", "--config", broken)
	out, err := command.CombinedOutput()
	if err == nil {
		t.Fatal("сервер поднялся с непонятным адресом в trustedProxies")
	}
	if code := command.ProcessState.ExitCode(); code != 78 {
		t.Fatalf("ждали код 78, чтобы systemd не крутил перезапуски, получили %d", code)
	}
	if !strings.Contains(string(out), "Перезапуск не поможет") {
		t.Fatalf("оператору не сказали, что перезапуск бесполезен:\n%s", out)
	}
}
