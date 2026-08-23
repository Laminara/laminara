package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/hwid"
)

func gateFor(t *testing.T, raw string) *hwid.Gate {
	t.Helper()
	var cfg hwid.Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("config: %v", err)
	}
	cfg.TicketSecretPath = filepath.Join(t.TempDir(), "ticket.key")
	gate, err := hwid.New(&cfg)
	if err != nil {
		t.Fatalf("new gate: %v", err)
	}
	t.Cleanup(func() { _ = gate.Close() })
	return gate
}

func seenMachine(t *testing.T, gate *hwid.Gate, identity hwid.Identity, seed byte) *apiv1.MachineReport {
	t.Helper()
	digest := func(value byte) []byte {
		out := make([]byte, 16)
		for i := range out {
			out[i] = value
		}
		return out
	}
	report := &apiv1.MachineReport{
		SchemaVersion: hwid.ReportSchemaVersion,
		Signals: []*apiv1.Signal{
			{Kind: apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, Digest: digest(seed), Confidence: 100},
			{Kind: apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, Digest: digest(seed + 1), Confidence: 100},
			{Kind: apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, Digest: digest(seed + 2), Confidence: 100},
		},
		Platform:             3,
		CollectedAtUnixNanos: time.Now().UnixNano(),
	}
	if _, err := gate.Check(context.Background(), identity, report, "127.0.0.1"); err != nil {
		t.Fatalf("first sight of the machine: %v", err)
	}
	return report
}

func run(t *testing.T, build command.Command, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	if err := build.Run(context.Background(), args, &out); err != nil {
		t.Fatalf("%v", err)
	}
	return out.String()
}

func TestMachinesFindsAPlayerWhoseSubjectIsNotTheNickname(t *testing.T) {
	gate := gateFor(t, `{"mode": "observe", "requireChallenge": false}`)
	seenMachine(t, gate, hwid.Identity{Subject: "eec62300-5d01", Username: "Strah"}, 20)

	for _, typed := range []string{"Strah", "strah", "STRAH"} {
		output := run(t, machinesCommand(gate), typed)
		if strings.Contains(output, "no machine seen") {
			t.Fatalf("machines %s found nothing: %s", typed, output)
		}
	}
}

func TestConsoleAccountBanRefusesTheNextLogin(t *testing.T) {
	gate := gateFor(t, `{"mode": "enforce", "requireChallenge": false}`)
	identity := hwid.Identity{Subject: "eec62300-5d01", Username: "Strah"}
	report := seenMachine(t, gate, identity, 40)

	output := run(t, banCommand(gate), "Strah", "cheating", "--account")
	if !strings.Contains(output, "banned") {
		t.Fatalf("ban did not report success: %s", output)
	}

	report.CollectedAtUnixNanos = time.Now().UnixNano()
	if _, err := gate.Check(context.Background(), identity, report, "127.0.0.1"); err == nil {
		t.Fatal("the account named at the console must be refused")
	}
}
