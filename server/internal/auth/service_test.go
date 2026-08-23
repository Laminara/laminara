package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/laminara/laminara/server/internal/auth"
)

type stubProvider struct {
	users map[string]string
}

func (p stubProvider) Authenticate(_ context.Context, creds auth.Credentials) (auth.Identity, error) {
	if password, ok := p.users[creds.Username]; ok && password == creds.Password {
		return auth.Identity{
			Subject:  creds.Username,
			Username: creds.Username,
			UUID:     auth.OfflineUUID(creds.Username),
		}, nil
	}
	return auth.Identity{}, auth.ErrInvalidCredentials
}

func newService() *auth.Service {
	provider := stubProvider{users: map[string]string{"neo": "matrix"}}
	return auth.NewService(provider, auth.NewMemorySessionStore(), auth.DefaultConfig())
}

func TestLoginAndValidate(t *testing.T) {
	ctx := context.Background()
	svc := newService()
	tokens, err := svc.Login(ctx, "neo", "matrix")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	identity, err := svc.ValidateAccess(ctx, tokens.Access)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if identity.Username != "neo" || identity.UUID == uuid.Nil {
		t.Fatalf("got %+v", identity)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	if _, err := newService().Login(context.Background(), "neo", "wrong"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestRefreshRotationAndReuse(t *testing.T) {
	ctx := context.Background()
	svc := newService()
	first, err := svc.Login(ctx, "neo", "matrix")
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Refresh(ctx, first.Refresh)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if second.Refresh == first.Refresh || second.Access == first.Access {
		t.Fatal("tokens were not rotated")
	}
	if _, err := svc.ValidateAccess(ctx, second.Access); err != nil {
		t.Fatalf("rotated access should be valid: %v", err)
	}
	if _, err := svc.Refresh(ctx, first.Refresh); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("reused refresh token should fail, got %v", err)
	}
	if _, err := svc.ValidateAccess(ctx, second.Access); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("reuse must revoke the whole session, got %v", err)
	}
}

func TestLogoutRevokes(t *testing.T) {
	ctx := context.Background()
	svc := newService()
	tokens, err := svc.Login(ctx, "neo", "matrix")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(ctx, tokens.Access); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := svc.ValidateAccess(ctx, tokens.Access); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("access after logout should fail, got %v", err)
	}
}
