package yggdrasil

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/redis/go-redis/v9"

	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/clientaddr"
	"github.com/laminara/laminara/server/internal/hwid"
	"github.com/laminara/laminara/server/internal/ratelimit"
	"github.com/laminara/laminara/server/internal/skin"
	"github.com/laminara/laminara/server/internal/version"
)

const MachineTicketHeader = "Laminara-Machine-Ticket"

const (
	tokenTTL = 24 * time.Hour
	joinTTL  = 30 * time.Second
)

type Config struct {
	ServerName  string
	SkinDomains []string
	RSAKeyPath  string
	Proxies     *clientaddr.Trust
	Sessions    *redis.Client
}

type Server struct {
	auth        *auth.Service
	machines    *hwid.Gate
	proxies     *clientaddr.Trust
	limits      *ratelimit.Guard
	skin        skin.Provider
	rsa         *rsa.PrivateKey
	publicPEM   string
	serverName  string
	skinDomains []string
	store       *store
	now         func() time.Time
}

func NewServer(authService *auth.Service, skinProvider skin.Provider, machines *hwid.Gate, limits *ratelimit.Guard, cfg Config) (*Server, error) {
	key, err := loadOrCreateRSA(cfg.RSAKeyPath)
	if err != nil {
		return nil, err
	}
	publicPEM, err := publicKeyPEM(key)
	if err != nil {
		return nil, err
	}
	now := time.Now
	return &Server{
		auth:        authService,
		machines:    machines,
		proxies:     cfg.Proxies,
		limits:      limits,
		skin:        skinProvider,
		rsa:         key,
		publicPEM:   publicPEM,
		serverName:  cfg.ServerName,
		skinDomains: cfg.SkinDomains,
		store:       newStore(now, cfg.Sessions),
		now:         now,
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.metadata)
	mux.HandleFunc("POST /authserver/authenticate", s.authenticate)
	mux.HandleFunc("POST /authserver/refresh", s.refresh)
	mux.HandleFunc("POST /authserver/validate", s.validate)
	mux.HandleFunc("POST /authserver/invalidate", s.invalidate)
	mux.HandleFunc("POST /authserver/signout", s.signout)
	mux.HandleFunc("POST /sessionserver/session/minecraft/join", s.join)
	mux.HandleFunc("GET /sessionserver/session/minecraft/hasJoined", s.hasJoined)
	mux.HandleFunc("GET /sessionserver/session/minecraft/profile/{uuid}", s.getProfile)
	mux.HandleFunc("POST /api/profiles/minecraft", s.profilesByName)
	return mux
}

func (s *Server) metadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"meta": map[string]any{
			"serverName":              s.serverName,
			"implementationName":      "laminara",
			"implementationVersion":   version.Current,
			"feature.non_email_login": true,
		},
		"skinDomains":        s.skinDomains,
		"signaturePublickey": s.publicPEM,
	})
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username      string `json:"username"`
		Password      string `json:"password"`
		ClientToken   string `json:"clientToken"`
		TwoFactorCode string `json:"twoFactorCode"`
		RequestUser   bool   `json:"requestUser"`
	}
	if !decode(w, r, &req) {
		return
	}
	address := s.proxies.OfRequest(r)
	if !s.limits.SignInAllowed(r.Context(), address, req.Username) {
		yggError(w, http.StatusTooManyRequests, "Too many attempts. Try again in a few minutes.")
		return
	}
	identity, err := s.auth.Verify(r.Context(), req.Username, req.Password, req.TwoFactorCode)
	if err != nil {
		if !errors.Is(err, auth.ErrInvalidCredentials) && !errors.Is(err, auth.ErrTwoFactorRequired) {
			yggError(w, http.StatusServiceUnavailable, "Источник аккаунтов недоступен — попробуйте позже.")
			return
		}
		if !errors.Is(err, auth.ErrTwoFactorRequired) {
			s.limits.SignInFailed(r.Context(), address, req.Username)
		}
		if errors.Is(err, auth.ErrTwoFactorRequired) {
			yggErrorType(w, http.StatusForbidden, "SecondFactorRequiredException", messageFor(err))
			return
		}
		yggError(w, http.StatusForbidden, messageFor(err))
		return
	}
	if err := s.machines.VerifyTicket(r.Header.Get(MachineTicketHeader), hwid.IdentityOf(identity)); err != nil {
		yggError(w, http.StatusForbidden, machineMessage(err))
		return
	}
	if err := s.machines.CheckSubject(r.Context(), hwid.IdentityOf(identity)); err != nil {
		yggError(w, http.StatusForbidden, machineMessage(err))
		return
	}
	clientToken := req.ClientToken
	if clientToken == "" {
		clientToken = randomToken()
	}
	accessToken := randomToken()
	if err := s.store.putSession(r.Context(), accessToken, clientToken, identity, tokenTTL); err != nil {
		yggError(w, http.StatusServiceUnavailable, "Хранилище игровых сессий недоступно — попробуйте позже.")
		return
	}
	s.store.rememberProfile(r.Context(), identity)

	s.writeSession(w, accessToken, session{clientToken: clientToken, identity: identity}, true, req.RequestUser)
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequestUser     bool `json:"requestUser"`
		SelectedProfile *struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"selectedProfile"`
		AccessToken string  `json:"accessToken"`
		ClientToken *string `json:"clientToken"`
	}
	if !decode(w, r, &req) {
		return
	}
	current, ok, err := s.store.session(r.Context(), req.AccessToken)
	if err != nil {
		yggError(w, http.StatusServiceUnavailable, "Хранилище игровых сессий недоступно — попробуйте позже.")
		return
	}
	if !ok || (req.ClientToken != nil && *req.ClientToken != current.clientToken) {
		yggError(w, http.StatusForbidden, "Токен недействителен.")
		return
	}
	if req.SelectedProfile != nil && (req.SelectedProfile.ID != dashless(current.identity.UUID) || req.SelectedProfile.Name != current.identity.Username) {
		yggError(w, http.StatusForbidden, "Выбранный профиль не принадлежит этому токену.")
		return
	}
	if req.SelectedProfile != nil {
		yggErrorType(w, http.StatusBadRequest, "IllegalArgumentException", "Access token already has a profile assigned.")
		return
	}
	newAccess := randomToken()
	sess, rotated, err := s.store.rotateSession(r.Context(), req.AccessToken, newAccess, tokenTTL)
	if err != nil {
		yggError(w, http.StatusServiceUnavailable, "Хранилище игровых сессий недоступно — попробуйте позже.")
		return
	}
	if !rotated {
		yggError(w, http.StatusForbidden, "Токен недействителен.")
		return
	}
	s.store.rememberProfile(r.Context(), sess.identity)
	s.writeSession(w, newAccess, sess, false, req.RequestUser)
}

func (s *Server) validate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccessToken string  `json:"accessToken"`
		ClientToken *string `json:"clientToken"`
	}
	if !decode(w, r, &req) {
		return
	}
	sess, ok, err := s.store.session(r.Context(), req.AccessToken)
	if err != nil {
		yggError(w, http.StatusServiceUnavailable, "Хранилище игровых сессий недоступно — попробуйте позже.")
		return
	}
	if !ok || (req.ClientToken != nil && *req.ClientToken != sess.clientToken) {
		yggError(w, http.StatusForbidden, "Токен недействителен.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) invalidate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccessToken string `json:"accessToken"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.store.deleteSession(r.Context(), req.AccessToken); err != nil {
		yggError(w, http.StatusServiceUnavailable, "Хранилище игровых сессий недоступно — попробуйте позже.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) signout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username      string `json:"username"`
		Password      string `json:"password"`
		TwoFactorCode string `json:"twoFactorCode"`
	}
	if !decode(w, r, &req) {
		return
	}
	address := s.proxies.OfRequest(r)
	if !s.limits.SignInAllowed(r.Context(), address, req.Username) {
		yggError(w, http.StatusTooManyRequests, "Too many attempts. Try again in a few minutes.")
		return
	}
	identity, err := s.auth.Verify(r.Context(), req.Username, req.Password, req.TwoFactorCode)
	if err != nil {
		if !errors.Is(err, auth.ErrInvalidCredentials) && !errors.Is(err, auth.ErrTwoFactorRequired) {
			yggError(w, http.StatusServiceUnavailable, "Источник аккаунтов недоступен — попробуйте позже.")
			return
		}
		if !errors.Is(err, auth.ErrTwoFactorRequired) {
			s.limits.SignInFailed(r.Context(), address, req.Username)
		}
		yggError(w, http.StatusForbidden, messageFor(err))
		return
	}
	if err := s.store.deleteUser(r.Context(), identity.Username); err != nil {
		yggError(w, http.StatusServiceUnavailable, "Хранилище игровых сессий недоступно — попробуйте позже.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) join(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccessToken     string `json:"accessToken"`
		ServerID        string `json:"serverId"`
		SelectedProfile string `json:"selectedProfile"`
	}
	if !decode(w, r, &req) {
		return
	}
	sess, ok, err := s.store.session(r.Context(), req.AccessToken)
	if err != nil {
		yggError(w, http.StatusServiceUnavailable, "Хранилище игровых сессий недоступно — попробуйте позже.")
		return
	}
	if !ok {
		yggError(w, http.StatusForbidden, "Токен недействителен.")
		return
	}
	if err := s.machines.CheckSubject(r.Context(), hwid.IdentityOf(sess.identity)); err != nil {
		yggError(w, http.StatusForbidden, machineMessage(err))
		return
	}
	if req.ServerID == "" || req.SelectedProfile != dashless(sess.identity.UUID) {
		yggError(w, http.StatusForbidden, "Выбран неверный профиль или не указан идентификатор сервера.")
		return
	}
	if err := s.store.putJoin(r.Context(), req.ServerID, req.AccessToken, s.proxies.OfRequest(r), sess.identity, joinTTL); err != nil {
		yggError(w, http.StatusServiceUnavailable, "Хранилище игровых сессий недоступно — попробуйте позже.")
		return
	}
	s.store.rememberProfile(r.Context(), sess.identity)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) hasJoined(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	serverID := r.URL.Query().Get("serverId")
	identity, ok, err := s.store.join(r.Context(), serverID, username, r.URL.Query().Get("ip"))
	if err != nil {
		yggError(w, http.StatusServiceUnavailable, "Хранилище игровых сессий недоступно — попробуйте позже.")
		return
	}
	if !ok || !strings.EqualFold(identity.Username, username) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.writeProfile(w, r, identity, true)
}

func (s *Server) getProfile(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("uuid"))
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	identity, ok, err := s.store.profile(r.Context(), dashless(id))
	if err != nil {
		yggError(w, http.StatusServiceUnavailable, "Хранилище профилей недоступно — попробуйте позже.")
		return
	}
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	signed := r.URL.Query().Get("unsigned") == "false"
	s.writeProfile(w, r, identity, signed)
}

func (s *Server) profilesByName(w http.ResponseWriter, r *http.Request) {
	var names []string
	if !decode(w, r, &names) {
		return
	}
	if names == nil {
		yggError(w, http.StatusBadRequest, "Нужен массив имён игроков.")
		return
	}
	if len(names) > 100 {
		yggError(w, http.StatusBadRequest, "За один запрос можно найти не больше 100 игроков.")
		return
	}
	resolver, ok := s.auth.Provider().(auth.ProfileLookup)
	if !ok {
		yggError(w, http.StatusServiceUnavailable, "Источник аккаунтов не поддерживает поиск профилей.")
		return
	}
	result := make([]map[string]string, 0, len(names))
	for _, name := range names {
		identity, err := resolver.LookupProfile(r.Context(), name)
		if errors.Is(err, auth.ErrInvalidCredentials) {
			continue
		}
		if err != nil {
			yggError(w, http.StatusServiceUnavailable, "Источник профилей недоступен — попробуйте позже.")
			return
		}
		result = append(result, gameProfile(identity))
		s.store.rememberProfile(r.Context(), identity)
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) writeProfile(w http.ResponseWriter, r *http.Request, identity auth.Identity, signed bool) {
	uuid := dashless(identity.UUID)
	properties := []property{}
	if textures, err := s.skin.Textures(r.Context(), identity.Username, identity.UUID.String()); err == nil {
		if prop, err := s.texturesProperty(uuid, identity.Username, textures); err == nil {
			if !signed {
				prop.Signature = ""
			}
			properties = append(properties, prop)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         uuid,
		"name":       identity.Username,
		"properties": properties,
	})
}

func (s *Server) writeSession(w http.ResponseWriter, accessToken string, sess session, available, requestUser bool) {
	profile := gameProfile(sess.identity)
	response := map[string]any{"accessToken": accessToken, "clientToken": sess.clientToken, "selectedProfile": profile}
	if available {
		response["availableProfiles"] = []map[string]string{profile}
	}
	if requestUser {
		response["user"] = map[string]any{"id": dashless(sess.identity.UUID), "properties": []any{}}
	}
	writeJSON(w, http.StatusOK, response)
}

func gameProfile(identity auth.Identity) map[string]string {
	return map[string]string{"id": dashless(identity.UUID), "name": identity.Username}
}

func dashless(id uuid.UUID) string {
	return strings.ReplaceAll(id.String(), "-", "")
}

func randomToken() string {
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func yggError(w http.ResponseWriter, status int, message string) {
	yggErrorType(w, status, "ForbiddenOperationException", message)
}

func yggErrorType(w http.ResponseWriter, status int, errorType, message string) {
	writeJSON(w, status, map[string]string{
		"error":        errorType,
		"errorMessage": message,
	})
}

func messageFor(err error) string {
	if errors.Is(err, auth.ErrTwoFactorRequired) {
		return "Нужен код из приложения-аутентификатора."
	}
	return "Неверный логин или пароль."
}

const maxYggBodyBytes = 64 << 10

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxYggBodyBytes)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(target); err != nil {
		yggError(w, http.StatusBadRequest, "Запрос составлен неверно.")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		yggError(w, http.StatusBadRequest, "Запрос составлен неверно.")
		return false
	}
	return true
}

func machineMessage(err error) string {
	if ban, ok := hwid.AsBanError(err); ok {
		return ban.Error()
	}
	switch {
	case errors.Is(err, hwid.ErrReportRequired),
		errors.Is(err, hwid.ErrReportInvalid),
		errors.Is(err, hwid.ErrChallengeStale),
		errors.Is(err, hwid.ErrVirtualMachine),
		errors.Is(err, hwid.ErrSoftwareKey):
		return err.Error()
	}
	slog.Default().Error("проверка компьютера не прошла из-за внутренней ошибки",
		"source", "yggdrasil",
		"ошибка", err,
	)
	return "Сервер не смог проверить компьютер — попробуйте позже."
}
