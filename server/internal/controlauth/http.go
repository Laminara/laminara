package controlauth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
)

func (s *Store) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, raw, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") {
			denied(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		token, err := s.Authenticate(raw)
		if err != nil {
			denied(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		if scope := rpcScope(method); scope != "" && !token.Allows(scope) {
			slog.Warn("административный запрос запрещён", "source", "audit", "actor", token.ID, "method", method)
			denied(w, http.StatusForbidden, "permission_denied")
			return
		}
		ctx, cancel := context.WithCancel(WithPrincipal(r.Context(), token))
		defer cancel()
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					current, err := s.Authenticate(raw)
					if err != nil || !slices.Equal(current.Scopes, token.Scopes) {
						cancel()
						return
					}
				}
			}
		}()
		writer := &auditWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(writer, r.WithContext(ctx))
		slog.Info("административный запрос завершён", "source", "audit", "actor", token.ID, "method", method, "status", writer.status)
	})
}

func denied(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": "нет административного доступа"})
}

type auditWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *auditWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *auditWriter) Write(data []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *auditWriter) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *auditWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
