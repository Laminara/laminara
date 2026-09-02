package webconsole

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/laminara/laminara/server/internal/auth/hash"
	"github.com/laminara/laminara/server/internal/duration"
)

const (
	basePath          = "/console"
	cookieName        = "laminara_console"
	defaultSessionTTL = 7 * 24 * time.Hour
	defaultLinkTTL    = 10 * time.Minute
	failureWindow     = time.Minute
	failureBudget     = 10
)

type Config struct {
	Enabled    *bool              `json:"enabled"`
	Auth       string             `json:"auth"`
	Password   string             `json:"password"`
	SessionTTL *duration.Duration `json:"sessionTTL"`
	LinkTTL    duration.Duration  `json:"linkTTL"`
	PublicURL  string             `json:"publicUrl"`
	StatePath  string             `json:"statePath"`
}

func (c *Config) On() bool {
	return c == nil || c.Enabled == nil || *c.Enabled
}

func (c *Config) mode() string {
	if c == nil {
		return "link"
	}
	switch strings.ToLower(strings.TrimSpace(c.Auth)) {
	case "password":
		return "password"
	case "both":
		return "both"
	default:
		return "link"
	}
}

type Service struct {
	cfg    *Config
	store  *store
	log    *slog.Logger
	binary string

	mu        sync.Mutex
	failures  map[string][]time.Time
	terminals int
}

func New(cfg *Config, log *slog.Logger) (*Service, error) {
	if !cfg.On() {
		return nil, nil
	}
	if cfg == nil {
		cfg = &Config{}
	}
	if cfg.mode() != "link" && cfg.Password == "" {
		return nil, fmt.Errorf("console.auth = %q, но console.password пуст — задайте хеш пароля (laminara-server hash)", cfg.Auth)
	}
	binary, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return &Service{
		cfg:      cfg,
		store:    newStore(cfg.StatePath),
		log:      log,
		binary:   binary,
		failures: map[string][]time.Time{},
	}, nil
}

func (s *Service) sessionTTL() time.Duration {
	if s.cfg.SessionTTL == nil {
		return defaultSessionTTL
	}
	return s.cfg.SessionTTL.Duration()
}

func (s *Service) linkTTL() time.Duration {
	if s.cfg.LinkTTL.Duration() > 0 {
		return s.cfg.LinkTTL.Duration()
	}
	return defaultLinkTTL
}

func (s *Service) Link() string {
	ticket := s.store.ticket(s.linkTTL())
	base := strings.TrimRight(strings.TrimSpace(s.cfg.PublicURL), "/")
	if base == "" {
		return fmt.Sprintf("%s/enter?t=%s (адрес допишите сами: console.publicUrl в конфиге не задан)", basePath, ticket)
	}
	return fmt.Sprintf("%s%s/enter?t=%s", base, basePath, ticket)
}

func (s *Service) Sessions() int {
	return s.store.live()
}

func (s *Service) Forget() int {
	return s.store.forgetAll()
}

func (s *Service) authorized(r *http.Request) bool {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	return s.store.valid(cookie.Value)
}

func (s *Service) allowAttempt(address string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	kept := s.failures[address][:0]
	for _, at := range s.failures[address] {
		if now.Sub(at) < failureWindow {
			kept = append(kept, at)
		}
	}
	s.failures[address] = kept
	return len(kept) < failureBudget
}

func (s *Service) noteFailure(address string) {
	s.mu.Lock()
	s.failures[address] = append(s.failures[address], time.Now())
	s.mu.Unlock()
}

func (s *Service) passwordMatches(candidate string) bool {
	if s.cfg.Password == "" || candidate == "" {
		return false
	}
	verifier, err := hash.Get("argon2id")
	if err != nil {
		return false
	}
	ok, err := verifier.Verify(candidate, s.cfg.Password)
	return err == nil && ok
}

func (s *Service) grant(w http.ResponseWriter, r *http.Request, secret string) {
	cookie := &http.Cookie{
		Name:     cookieName,
		Value:    secret,
		Path:     basePath,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure(r),
	}
	if ttl := s.sessionTTL(); ttl > 0 {
		cookie.Expires = time.Now().Add(ttl)
	}
	http.SetCookie(w, cookie)
}

func secure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func address(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
