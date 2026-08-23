package yggdrasil

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/laminara/laminara/server/internal/auth"
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
}

type Server struct {
	auth        *auth.Service
	machines    *hwid.Gate
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
		limits:      limits,
		skin:        skinProvider,
		rsa:         key,
		publicPEM:   publicPEM,
		serverName:  cfg.ServerName,
		skinDomains: cfg.SkinDomains,
		store:       newStore(now),
		now:         now,
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.metadata)
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
		Username    string `json:"username"`
		Password    string `json:"password"`
		ClientToken string `json:"clientToken"`
	}
	if !decode(w, r, &req) {
		return
	}
	address := clientAddress(r)
	if !s.limits.SignInAllowed(r.Context(), address, req.Username) {
		yggError(w, http.StatusTooManyRequests, "Too many attempts. Try again in a few minutes.")
		return
	}
	identity, err := s.auth.Verify(r.Context(), req.Username, req.Password)
	if err != nil {
		s.limits.SignInFailed(r.Context(), address, req.Username)
		yggError(w, http.StatusForbidden, "Invalid credentials. Invalid username or password.")
		return
	}
	if err := s.machines.VerifyTicket(r.Header.Get(MachineTicketHeader)); err != nil {
		yggError(w, http.StatusForbidden, err.Error())
		return
	}
	if err := s.machines.CheckSubject(r.Context(), hwid.IdentityOf(identity)); err != nil {
		yggError(w, http.StatusForbidden, err.Error())
		return
	}
	clientToken := req.ClientToken
	if clientToken == "" {
		clientToken = randomToken()
	}
	accessToken := randomToken()
	s.store.putSession(accessToken, clientToken, identity, tokenTTL)
	s.store.rememberProfile(identity)

	profile := gameProfile(identity)
	writeJSON(w, http.StatusOK, map[string]any{
		"accessToken":       accessToken,
		"clientToken":       clientToken,
		"availableProfiles": []map[string]string{profile},
		"selectedProfile":   profile,
	})
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccessToken string `json:"accessToken"`
		ClientToken string `json:"clientToken"`
	}
	if !decode(w, r, &req) {
		return
	}
	current, ok := s.store.session(req.AccessToken)
	if !ok || (req.ClientToken != "" && req.ClientToken != current.clientToken) {
		yggError(w, http.StatusForbidden, "Invalid token.")
		return
	}
	newAccess := randomToken()
	sess, _ := s.store.rotateSession(req.AccessToken, newAccess, tokenTTL)
	writeJSON(w, http.StatusOK, map[string]any{
		"accessToken":     newAccess,
		"clientToken":     sess.clientToken,
		"selectedProfile": gameProfile(sess.identity),
	})
}

func (s *Server) validate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccessToken string `json:"accessToken"`
		ClientToken string `json:"clientToken"`
	}
	if !decode(w, r, &req) {
		return
	}
	sess, ok := s.store.session(req.AccessToken)
	if !ok || (req.ClientToken != "" && req.ClientToken != sess.clientToken) {
		yggError(w, http.StatusForbidden, "Invalid token.")
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
	s.store.deleteSession(req.AccessToken)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) signout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	if _, err := s.auth.Verify(r.Context(), req.Username, req.Password); err != nil {
		yggError(w, http.StatusForbidden, "Invalid credentials.")
		return
	}
	s.store.deleteUser(req.Username)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) join(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccessToken string `json:"accessToken"`
		ServerID    string `json:"serverId"`
	}
	if !decode(w, r, &req) {
		return
	}
	sess, ok := s.store.session(req.AccessToken)
	if !ok {
		yggError(w, http.StatusForbidden, "Invalid token.")
		return
	}
	if err := s.machines.CheckSubject(r.Context(), hwid.IdentityOf(sess.identity)); err != nil {
		yggError(w, http.StatusForbidden, err.Error())
		return
	}
	s.store.putJoin(req.ServerID, sess.identity, joinTTL)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) hasJoined(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	serverID := r.URL.Query().Get("serverId")
	identity, ok := s.store.join(serverID)
	if !ok || !strings.EqualFold(identity.Username, username) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.writeProfile(w, r, identity, true)
}

func (s *Server) getProfile(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.store.profile(r.PathValue("uuid"))
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
	result := make([]map[string]string, 0, len(names))
	for _, name := range names {
		result = append(result, map[string]string{"id": dashless(auth.OfflineUUID(name)), "name": name})
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) writeProfile(w http.ResponseWriter, r *http.Request, identity auth.Identity, signed bool) {
	uuid := dashless(identity.UUID)
	properties := []property{}
	if textures, err := s.skin.Textures(r.Context(), identity.Username, uuid); err == nil {
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
	writeJSON(w, status, map[string]string{
		"error":        "ForbiddenOperationException",
		"errorMessage": message,
	})
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		yggError(w, http.StatusBadRequest, "Malformed request body.")
		return false
	}
	return true
}

func clientAddress(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		if trimmed := strings.TrimSpace(first); trimmed != "" {
			return trimmed
		}
	}
	if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
		return real
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
