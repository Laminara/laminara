package ratelimit_test

import (
	"context"
	"testing"

	"github.com/laminara/laminara/server/internal/ratelimit"
)

func TestSuccessfulSignInNeverSpendsBudget(t *testing.T) {
	guard, err := ratelimit.New(&ratelimit.Config{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for range 500 {
		if !guard.SignInAllowed(ctx, "10.0.0.1", "neo") {
			t.Fatal("a run of successful sign-ins must not exhaust anything")
		}
	}
}

func TestBruteForceIsStoppedPerAddress(t *testing.T) {
	guard, err := ratelimit.New(&ratelimit.Config{Login: ratelimit.Bucket{Limit: 3}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for range 3 {
		if !guard.SignInAllowed(ctx, "10.0.0.2", "neo") {
			t.Fatal("attempts within the budget must be allowed")
		}
		guard.SignInFailed(ctx, "10.0.0.2", "neo")
	}
	if guard.SignInAllowed(ctx, "10.0.0.2", "neo") {
		t.Fatal("the address must be blocked once its budget is gone")
	}
	if !guard.SignInAllowed(ctx, "10.0.0.3", "neo") {
		t.Fatal("one attacker must not lock out every other address")
	}
}

func TestChallengesAreBudgeted(t *testing.T) {
	guard, err := ratelimit.New(&ratelimit.Config{Challenge: ratelimit.Bucket{Limit: 2}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if !guard.ChallengeAllowed(ctx, "10.0.0.4") || !guard.ChallengeAllowed(ctx, "10.0.0.4") {
		t.Fatal("challenges within the budget must be allowed")
	}
	if guard.ChallengeAllowed(ctx, "10.0.0.4") {
		t.Fatal("challenge flooding must be stopped")
	}
}

func TestDisabledGuardAllowsEverything(t *testing.T) {
	guard, err := ratelimit.New(&ratelimit.Config{Disabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if guard != nil {
		t.Fatal("a disabled guard must be nil so the checks compile away to nothing")
	}
	for range 100 {
		guard.SignInFailed(context.Background(), "10.0.0.5", "neo")
		if !guard.SignInAllowed(context.Background(), "10.0.0.5", "neo") {
			t.Fatal("a disabled guard must never refuse")
		}
	}
}
