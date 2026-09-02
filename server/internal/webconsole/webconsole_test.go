package webconsole

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/laminara/laminara/server/internal/auth/hash"
	"github.com/laminara/laminara/server/internal/duration"
)

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func serve(t *testing.T, cfg *Config) (*Service, *httptest.Server) {
	t.Helper()
	if cfg.StatePath == "" {
		cfg.StatePath = filepath.Join(t.TempDir(), "sessions.json")
	}
	service, err := New(cfg, quiet())
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	service.Mount(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	if cfg.PublicURL == "" {
		service.cfg.PublicURL = server.URL
	}
	return service, server
}

func client(server *httptest.Server) *http.Client {
	jar, _ := newJar()
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func newJar() (http.CookieJar, error) {
	return cookieJar{store: map[string][]*http.Cookie{}}, nil
}

type cookieJar struct {
	store map[string][]*http.Cookie
}

func (j cookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.store[u.Host] = append(j.store[u.Host], cookies...)
}

func (j cookieJar) Cookies(u *url.URL) []*http.Cookie {
	return j.store[u.Host]
}

func TestLinkOpensTheConsoleOnce(t *testing.T) {
	service, server := serve(t, &Config{})
	browser := client(server)

	link := service.Link()
	first, err := browser.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Body.Close()
	if first.StatusCode != http.StatusFound {
		t.Fatalf("вход по ссылке = %d, ждали переход", first.StatusCode)
	}

	page, err := browser.Get(server.URL + basePath + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer page.Body.Close()
	body, _ := io.ReadAll(page.Body)
	if page.StatusCode != http.StatusOK || !strings.Contains(string(body), "консоль сервера") {
		t.Fatalf("страница консоли = %d %s", page.StatusCode, body)
	}

	second, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Get(link)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Body.Close()
	if second.StatusCode != http.StatusNotFound {
		t.Fatalf("вторая попытка по той же ссылке = %d, ждали 404", second.StatusCode)
	}
}

func TestWithoutSessionEverythingIsMissing(t *testing.T) {
	_, server := serve(t, &Config{})
	stranger := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	for _, path := range []string{basePath + "/", basePath + "/socket", basePath + "/app.js", basePath + "/enter?t=подделка"} {
		response, err := stranger.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s без сеанса = %d, ждали 404", path, response.StatusCode)
		}
	}
}

func TestExpiredLinkIsRefused(t *testing.T) {
	service, server := serve(t, &Config{LinkTTL: duration.Duration(time.Millisecond)})
	link := service.Link()
	time.Sleep(5 * time.Millisecond)

	response, err := client(server).Get(link)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("протухшая ссылка = %d, ждали 404", response.StatusCode)
	}
}

func TestPasswordMode(t *testing.T) {
	stored, err := hash.Produce("argon2id", "правильный")
	if err != nil {
		t.Fatal(err)
	}
	_, server := serve(t, &Config{Auth: "password", Password: stored})
	browser := client(server)

	page, err := browser.Get(server.URL + basePath + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if page.StatusCode != http.StatusOK || !strings.Contains(string(body), "Пароль") {
		t.Fatalf("страница входа = %d", page.StatusCode)
	}

	wrong, err := browser.PostForm(server.URL+basePath+"/login", url.Values{"password": {"мимо"}})
	if err != nil {
		t.Fatal(err)
	}
	wrong.Body.Close()
	if !strings.Contains(wrong.Header.Get("Location"), "wrong=1") {
		t.Fatalf("неверный пароль ведёт на %q", wrong.Header.Get("Location"))
	}

	right, err := browser.PostForm(server.URL+basePath+"/login", url.Values{"password": {"правильный"}})
	if err != nil {
		t.Fatal(err)
	}
	right.Body.Close()
	if right.StatusCode != http.StatusFound {
		t.Fatalf("верный пароль = %d", right.StatusCode)
	}

	console, err := browser.Get(server.URL + basePath + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer console.Body.Close()
	shown, _ := io.ReadAll(console.Body)
	if !strings.Contains(string(shown), "консоль сервера") {
		t.Fatal("после входа по паролю должна открываться консоль")
	}
}

func TestPasswordModeNeedsAPassword(t *testing.T) {
	if _, err := New(&Config{Auth: "password"}, quiet()); err == nil {
		t.Fatal("режим пароля без пароля должен быть отвергнут")
	}
}

func TestSessionsSurviveARestart(t *testing.T) {
	state := filepath.Join(t.TempDir(), "sessions.json")
	service, server := serve(t, &Config{StatePath: state})
	browser := client(server)

	response, err := browser.Get(service.Link())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()

	restarted, err := New(&Config{StatePath: state, PublicURL: server.URL}, quiet())
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Sessions() != 1 {
		t.Fatalf("после перезапуска осталось %d сеансов, ждали 1", restarted.Sessions())
	}
	if restarted.Forget() != 1 {
		t.Fatal("сеансы должны закрываться командой")
	}
	if restarted.Sessions() != 0 {
		t.Fatal("после сброса сеансов быть не должно")
	}
}

func TestDisabledConsoleIsNotBuilt(t *testing.T) {
	off := false
	service, err := New(&Config{Enabled: &off}, quiet())
	if err != nil {
		t.Fatal(err)
	}
	if service != nil {
		t.Fatal("выключенная консоль не должна подниматься")
	}
	mux := http.NewServeMux()
	service.Mount(mux)
}
