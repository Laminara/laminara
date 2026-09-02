package webconsole

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
)

//go:embed assets
var assets embed.FS

const (
	readLimit     = 1 << 20
	idleTimeout   = 4 * time.Hour
	maxTerminals  = 8
	defaultCols   = 120
	defaultRows   = 32
	shutdownGrace = 2 * time.Second
)

func (s *Service) Mount(mux *http.ServeMux) {
	if s == nil {
		return
	}
	mux.HandleFunc(basePath+"/enter", s.enter)
	mux.HandleFunc(basePath+"/login", s.login)
	mux.HandleFunc(basePath+"/socket", s.socket)
	mux.HandleFunc(basePath+"/", s.page)
	mux.HandleFunc(basePath, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, basePath+"/", http.StatusFound)
	})
}

func (s *Service) enter(w http.ResponseWriter, r *http.Request) {
	ticket := r.URL.Query().Get("t")
	from := address(r)
	if !s.allowAttempt(from) {
		http.NotFound(w, r)
		return
	}
	session, err := s.store.redeem(ticket, from, s.sessionTTL())
	if err != nil {
		s.noteFailure(from)
		s.log.Warn("вход в консоль по негодной ссылке", "source", "console", "адрес", from)
		http.NotFound(w, r)
		return
	}
	s.grant(w, r, session)
	s.log.Info("вход в веб-консоль по ссылке", "source", "console", "адрес", from)
	http.Redirect(w, r, basePath+"/", http.StatusFound)
}

func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || s.cfg.mode() == "link" {
		http.NotFound(w, r)
		return
	}
	from := address(r)
	if !s.allowAttempt(from) {
		http.Error(w, "слишком много попыток — подождите минуту", http.StatusTooManyRequests)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "не разобрал форму", http.StatusBadRequest)
		return
	}
	if !s.passwordMatches(r.PostFormValue("password")) {
		s.noteFailure(from)
		s.log.Warn("неверный пароль веб-консоли", "source", "console", "адрес", from)
		http.Redirect(w, r, basePath+"/?wrong=1", http.StatusFound)
		return
	}
	s.grant(w, r, s.store.open(from, s.sessionTTL()))
	s.log.Info("вход в веб-консоль по паролю", "source", "console", "адрес", from)
	http.Redirect(w, r, basePath+"/", http.StatusFound)
}

func (s *Service) page(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, basePath+"/")
	if name != "" {
		s.static(w, r, name)
		return
	}
	if !s.authorized(r) {
		if s.cfg.mode() == "link" {
			http.NotFound(w, r)
			return
		}
		s.serve(w, "assets/login.html", "text/html; charset=utf-8")
		return
	}
	s.serve(w, "assets/console.html", "text/html; charset=utf-8")
}

func (s *Service) static(w http.ResponseWriter, r *http.Request, name string) {
	if !s.authorized(r) && s.cfg.mode() == "link" {
		http.NotFound(w, r)
		return
	}
	body, err := assets.ReadFile("assets/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType(name))
	w.Header().Set("Cache-Control", "no-store")
	w.Write(body)
}

func (s *Service) serve(w http.ResponseWriter, name, kind string) {
	body, err := fs.ReadFile(assets, name)
	if err != nil {
		http.Error(w, "страница потерялась", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(body)
}

func contentType(name string) string {
	switch {
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "text/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func (s *Service) socket(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.NotFound(w, r)
		return
	}
	if !s.claimTerminal() {
		http.Error(w, "открыто слишком много консолей", http.StatusTooManyRequests)
		return
	}
	defer s.releaseTerminal()

	connection, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return
	}
	defer connection.CloseNow()
	connection.SetReadLimit(readLimit)

	ctx, cancel := context.WithTimeout(r.Context(), idleTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, s.binary, "console")
	command.Env = append(os.Environ(), "TERM=xterm-256color", "LAMINARA_CONSOLE_WEB=1")
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Cols: defaultCols, Rows: defaultRows})
	if err != nil {
		connection.Close(websocket.StatusInternalError, "консоль не запустилась")
		s.log.Error("веб-консоль не смогла запустить сеанс", "source", "console", "error", err)
		return
	}
	defer terminal.Close()

	from := address(r)
	s.log.Info("веб-консоль открыта", "source", "console", "адрес", from)
	defer s.log.Info("веб-консоль закрыта", "source", "console", "адрес", from)

	go pump(ctx, connection, terminal)

	buffer := make([]byte, 4096)
	for {
		read, err := terminal.Read(buffer)
		if read > 0 {
			if err := connection.Write(ctx, websocket.MessageBinary, buffer[:read]); err != nil {
				return
			}
		}
		if err != nil {
			connection.Close(websocket.StatusNormalClosure, "сеанс завершён")
			return
		}
	}
}

func pump(ctx context.Context, connection *websocket.Conn, terminal *os.File) {
	for {
		kind, payload, err := connection.Read(ctx)
		if err != nil {
			terminal.Close()
			return
		}
		switch kind {
		case websocket.MessageBinary:
			if _, err := terminal.Write(payload); err != nil {
				return
			}
		case websocket.MessageText:
			resize(terminal, payload)
		}
	}
}

func resize(terminal *os.File, payload []byte) {
	width, height, ok := strings.Cut(strings.TrimSpace(string(payload)), "x")
	if !ok {
		return
	}
	cols, colErr := strconv.Atoi(width)
	rows, rowErr := strconv.Atoi(height)
	if colErr != nil || rowErr != nil || cols <= 0 || rows <= 0 || cols > 1000 || rows > 1000 {
		return
	}
	_ = pty.Setsize(terminal, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (s *Service) claimTerminal() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terminals >= maxTerminals {
		return false
	}
	s.terminals++
	return true
}

func (s *Service) releaseTerminal() {
	s.mu.Lock()
	s.terminals--
	s.mu.Unlock()
}
