package duration_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/laminara/laminara/server/internal/duration"
)

func TestParse(t *testing.T) {
	for value, want := range map[string]time.Duration{
		"30s":   30 * time.Second,
		"15m":   15 * time.Minute,
		"12h":   12 * time.Hour,
		"30d":   30 * 24 * time.Hour,
		"90d":   90 * 24 * time.Hour,
		" 15m ": 15 * time.Minute,
		"1h30m": 90 * time.Minute,
		"0s":    0,
	} {
		parsed, err := duration.Parse(value)
		if err != nil {
			t.Fatalf("%s: %v", value, err)
		}
		if parsed.Duration() != want {
			t.Fatalf("%s = %s, want %s", value, parsed, time.Duration(want))
		}
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, value := range []string{"", "30", "12д", "d", "1dd", "30x", "много"} {
		parsed, err := duration.Parse(value)
		if err == nil {
			t.Fatalf("%q must be refused, got %s", value, parsed)
		}
		if !strings.Contains(err.Error(), value) {
			t.Fatalf("error must name the value: %v", err)
		}
	}
}

func TestJSONRoundTrip(t *testing.T) {
	var payload struct {
		TTL duration.Duration `json:"ttl"`
	}
	if err := json.Unmarshal([]byte(`{"ttl":"30d"}`), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TTL.Duration() != 30*24*time.Hour {
		t.Fatalf("ttl = %s", payload.TTL)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"ttl":"720h"}` {
		t.Fatalf("encoded = %s", encoded)
	}
	if err := json.Unmarshal([]byte(`{"ttl":"later"}`), &payload); err == nil {
		t.Fatal("a word is not a duration")
	}
}

func TestCompact(t *testing.T) {
	for value, want := range map[string]string{
		"720h":   "720h",
		"15m":    "15m",
		"30s":    "30s",
		"90m":    "1h30m",
		"1h0m5s": "1h5s",
		"500ms":  "500ms",
		"0s":     "0s",
	} {
		parsed, err := duration.Parse(value)
		if err != nil {
			t.Fatalf("%s: %v", value, err)
		}
		if got := parsed.Compact(); got != want {
			t.Fatalf("%s compacts to %s, want %s", value, got, want)
		}
	}
}
