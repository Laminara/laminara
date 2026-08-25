package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
	"github.com/laminara/laminara/gen/go/laminara/api/v1/apiv1connect"
	"github.com/laminara/laminara/server/internal/access"
	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/catalog"
	"github.com/laminara/laminara/server/internal/hwid"
	"github.com/laminara/laminara/server/internal/launchersvc"
	"github.com/laminara/laminara/server/internal/news"
	"github.com/laminara/laminara/server/internal/ratelimit"
	"github.com/laminara/laminara/server/internal/storage"
)

type Service struct {
	auth     *auth.Service
	catalog  *catalog.Catalog
	releases *launchersvc.Releases
	access   *access.Controller
	machines *hwid.Gate
	limits   *ratelimit.Guard
	news     *news.Service
	tokens   *tokenCache
}

type Options struct {
	Auth     *auth.Service
	Catalog  *catalog.Catalog
	Releases *launchersvc.Releases
	Access   *access.Controller
	Machines *hwid.Gate
	Limits   *ratelimit.Guard
	News     *news.Service
}

func NewService(opts Options) *Service {
	return &Service{
		auth:     opts.Auth,
		catalog:  opts.Catalog,
		releases: opts.Releases,
		access:   opts.Access,
		machines: opts.Machines,
		limits:   opts.Limits,
		news:     opts.News,
		tokens:   newTokenCache(),
	}
}

func (s *Service) GetNews(ctx context.Context, _ *connect.Request[apiv1.GetNewsRequest]) (*connect.Response[apiv1.GetNewsResponse], error) {
	return connect.NewResponse(&apiv1.GetNewsResponse{Items: s.news.Latest(ctx)}), nil
}

func (s *Service) CheckUpdate(_ context.Context, _ *connect.Request[apiv1.CheckUpdateRequest]) (*connect.Response[apiv1.CheckUpdateResponse], error) {
	canonical, signature, err := s.releases.Current()
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&apiv1.CheckUpdateResponse{Release: canonical, Signature: signature}), nil
}

func Handler(service *Service, backend storage.Backend, xAccel bool) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(apiv1connect.NewLauncherServiceHandler(service))
	mux.Handle("/objects/", service.ObjectHandler(backend, xAccel))
	return mux
}

func (s *Service) Login(ctx context.Context, req *connect.Request[apiv1.LoginRequest]) (*connect.Response[apiv1.LoginResponse], error) {
	if s.auth == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("auth is not configured"))
	}
	address := clientIP(req.Header(), req.Peer().Addr)
	if !s.limits.SignInAllowed(ctx, address, req.Msg.Username) {
		return nil, connect.NewError(connect.CodeResourceExhausted, errTooManyAttempts)
	}
	tokens, err := s.auth.Login(ctx, req.Msg.Username, req.Msg.Password)
	if err != nil {
		s.limits.SignInFailed(ctx, address, req.Msg.Username)
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	if !s.machines.Enabled() {
		return connect.NewResponse(&apiv1.LoginResponse{Tokens: toTokens(tokens)}), nil
	}
	identity, err := s.auth.ValidateAccess(ctx, tokens.Access)
	if err != nil {
		return nil, err
	}
	verdict, err := s.machines.Check(ctx, hwid.IdentityOf(identity), req.Msg.Machine, clientIP(req.Header(), req.Peer().Addr))
	if err != nil {
		_ = s.auth.Logout(ctx, tokens.Access)
		return nil, machineError(err)
	}
	return connect.NewResponse(&apiv1.LoginResponse{Tokens: toTokens(tokens), Machine: verdict}), nil
}

func (s *Service) Refresh(ctx context.Context, req *connect.Request[apiv1.RefreshRequest]) (*connect.Response[apiv1.RefreshResponse], error) {
	if s.auth == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("auth is not configured"))
	}
	tokens, err := s.auth.Refresh(ctx, req.Msg.Refresh)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	return connect.NewResponse(&apiv1.RefreshResponse{Tokens: toTokens(tokens)}), nil
}

func (s *Service) ListProfiles(ctx context.Context, req *connect.Request[apiv1.ListProfilesRequest]) (*connect.Response[apiv1.ListProfilesResponse], error) {
	summaries, err := s.catalog.Summaries(req.Msg.Platform)
	if err != nil {
		return nil, err
	}
	subject, _ := s.subjectOf(ctx, req.Header())
	memo := s.access.Memo(subject)
	resp := &apiv1.ListProfilesResponse{}
	for _, summary := range summaries {
		decision := memo.Decide(ctx, summary.Name)
		if !decision.Allowed && decision.Hidden {
			continue
		}
		resp.Profiles = append(resp.Profiles, &apiv1.ProfileSummary{
			Name:             summary.Name,
			Version:          summary.Version,
			MinecraftVersion: summary.Minecraft,
			TotalSize:        summary.TotalSize,
			ServerAddress:    summary.ServerAddress,
			Loader:           summary.Loader,
			HasFeatures:      summary.HasFeatures,
			Platforms:        summary.Platforms,
			Locked:           !decision.Allowed,
			LockReason:       decision.Reason,
		})
	}
	return connect.NewResponse(resp), nil
}

func (s *Service) GetManifest(ctx context.Context, req *connect.Request[apiv1.GetManifestRequest]) (*connect.Response[apiv1.GetManifestResponse], error) {
	subject, state := s.subjectOf(ctx, req.Header())
	if state == tokenStale {
		return nil, connect.NewError(connect.CodeUnauthenticated, errStaleSession)
	}
	decision := s.access.Decide(ctx, req.Msg.Profile, subject)
	if !decision.Allowed {
		if decision.Hidden {
			return nil, connect.NewError(connect.CodeNotFound, catalog.ErrNotFound)
		}
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New(decision.Reason))
	}
	canonical, signature, err := s.catalog.Get(req.Msg.Profile, req.Msg.Platform)
	if errors.Is(err, catalog.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if errors.Is(err, catalog.ErrPlatformUnavailable) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&apiv1.GetManifestResponse{Manifest: canonical, Signature: signature}), nil
}

func (s *Service) allowedObject(r *http.Request, key string) (allowed bool, stale bool) {
	if !s.access.Guarded() {
		return true, false
	}
	subject, state := s.subjectOf(r.Context(), r.Header)
	if state == tokenStale {
		return false, true
	}
	owners, err := s.catalog.Owners(key)
	if err != nil {
		return false, false
	}
	memo := s.access.Memo(subject)
	for _, owner := range owners {
		if memo.Decide(r.Context(), owner).Allowed {
			return true, false
		}
	}
	return len(owners) == 0 && s.isLauncherArtifact(key), false
}

func (s *Service) isLauncherArtifact(key string) bool {
	releases, err := s.releases.All()
	if err != nil {
		return false
	}
	for _, release := range releases {
		for _, artifact := range release.Artifacts {
			if artifact.Object == nil || artifact.Object.Hash == nil {
				continue
			}
			if storage.ObjectKey(artifact.Object.Hash.Algo, artifact.Object.Hash.Value) == key {
				return true
			}
		}
	}
	return false
}

func (s *Service) ObjectHandler(backend storage.Backend, xAccel bool) http.Handler {
	serve := ObjectHandler(backend, xAccel)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowed, stale := s.allowedObject(r, strings.TrimPrefix(r.URL.Path, "/objects/"))
		if stale {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, errStaleSession.Error(), http.StatusUnauthorized)
			return
		}
		if !allowed {
			http.Error(w, "object not found", http.StatusNotFound)
			return
		}
		serve.ServeHTTP(w, r)
	})
}

func ObjectHandler(backend storage.Backend, xAccel bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/objects/")
		if key == "" || strings.Contains(key, "..") {
			http.Error(w, "bad object key", http.StatusBadRequest)
			return
		}
		location, err := backend.Locate(r.Context(), key, time.Hour)
		if err == nil && location.Kind == storage.LocationURL {
			http.Redirect(w, r, location.URL, http.StatusFound)
			return
		}
		if xAccel && err == nil && location.Kind == storage.LocationInternal && location.InternalPath != "" {
			w.Header().Set("Cache-Control", "public, immutable, max-age=31536000")
			w.Header().Set("X-Accel-Redirect", location.InternalPath)
			return
		}
		reader, err := backend.Get(r.Context(), key)
		if err != nil {
			http.Error(w, "object not found", http.StatusNotFound)
			return
		}
		defer reader.Close()
		w.Header().Set("Cache-Control", "public, immutable, max-age=31536000")
		_, _ = io.Copy(w, reader)
	})
}

func toTokens(tokens *auth.Tokens) *apiv1.Tokens {
	return &apiv1.Tokens{
		Access:                  tokens.Access,
		AccessExpiresUnixNanos:  tokens.AccessExpires.UnixNano(),
		Refresh:                 tokens.Refresh,
		RefreshExpiresUnixNanos: tokens.RefreshExpires.UnixNano(),
	}
}
