package remote

import (
	"context"
	"errors"
	"testing"

	sdkmodule "github.com/laminara/laminara/sdk/go/module"
	"github.com/laminara/laminara/server/internal/auth"
)

func (f *fakeService) LookupProfile(context.Context, sdkmodule.Handle, string) (sdkmodule.Identity, error) {
	return f.identity, f.authErr
}

func TestRemoteAuthProfileLookup(t *testing.T) {
	svc := &fakeService{identity: sdkmodule.Identity{Subject: "42", Username: "Neo", UUID: "d1b0f4a2-0000-4000-8000-000000000001"}}
	provider := &remoteAuth{svc: svc, handle: 7}
	resolver, ok := any(provider).(auth.ProfileLookup)
	if !ok {
		t.Fatal("module auth provider does not support profile lookup")
	}
	profile, err := resolver.LookupProfile(t.Context(), "neo")
	if err != nil {
		t.Fatal(err)
	}
	login, err := provider.Authenticate(t.Context(), auth.Credentials{Username: "neo"})
	if err != nil || profile != login {
		t.Fatalf("lookup/login mismatch: %+v %+v %v", profile, login, err)
	}
	svc.authErr = sdkmodule.ErrInvalidCredentials
	if _, err := resolver.LookupProfile(t.Context(), "missing"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("unknown account: %v", err)
	}
	svc.authErr = errors.New("source unavailable")
	if _, err := resolver.LookupProfile(t.Context(), "neo"); !errors.Is(err, svc.authErr) {
		t.Fatalf("outage hidden: %v", err)
	}
	svc.authErr = nil
	for _, id := range []string{"broken", "00000000-0000-0000-0000-000000000000"} {
		svc.identity.UUID = id
		if _, err := resolver.LookupProfile(t.Context(), "neo"); err == nil {
			t.Fatalf("invalid UUID accepted: %s", id)
		}
		if _, err := provider.Authenticate(t.Context(), auth.Credentials{Username: "neo"}); err == nil {
			t.Fatalf("invalid login UUID accepted: %s", id)
		}
	}
	svc.identity.UUID = ""
	profile, err = resolver.LookupProfile(t.Context(), "neo")
	if err != nil || profile.UUID != auth.OfflineUUID("Neo") {
		t.Fatalf("empty UUID fallback: %+v %v", profile, err)
	}
	svc.identity.Username = "Other"
	if _, err := resolver.LookupProfile(t.Context(), "neo"); err == nil {
		t.Fatal("unrelated profile accepted")
	}
}
