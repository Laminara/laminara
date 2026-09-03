package hwid_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
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
	if gate == nil {
		t.Fatal("expected a gate")
	}
	t.Cleanup(func() { _ = gate.Close() })
	return gate
}

func digest(seed byte) []byte {
	out := make([]byte, 16)
	for i := range out {
		out[i] = seed
	}
	return out
}

func signal(kind apiv1.SignalKind, seed byte) *apiv1.Signal {
	return &apiv1.Signal{Kind: kind, Digest: digest(seed), Confidence: 100}
}

func reportOf(signals ...*apiv1.Signal) *apiv1.MachineReport {
	return &apiv1.MachineReport{
		SchemaVersion:        hwid.ReportSchemaVersion,
		Signals:              signals,
		Platform:             3,
		CollectedAtUnixNanos: time.Now().UnixNano(),
	}
}

func check(t *testing.T, gate *hwid.Gate, username string, report *apiv1.MachineReport) (*apiv1.MachineVerdict, error) {
	t.Helper()
	return gate.Check(context.Background(), hwid.Identity{Subject: username, Username: username}, report, "127.0.0.1")
}

func TestOffGateDoesNothing(t *testing.T) {
	gate, err := hwid.New(&hwid.Config{Mode: hwid.ModeOff})
	if err != nil {
		t.Fatal(err)
	}
	if gate.Enabled() {
		t.Fatal("mode off must produce no gate at all")
	}
	if verdict, err := gate.Check(context.Background(), hwid.Identity{Username: "neo"}, nil, ""); err != nil || verdict != nil {
		t.Fatalf("a disabled gate must be inert, got %v %v", verdict, err)
	}
}

func TestSameMachineIsRecognisedAcrossAccounts(t *testing.T) {
	gate := gateFor(t, `{"mode": "enforce", "requireChallenge": false}`)
	report := reportOf(
		signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 1),
		signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 2),
		signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, 3),
	)

	first, err := check(t, gate, "neo", report)
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	if !first.FirstSeen {
		t.Fatal("a machine nobody has seen must be reported as new")
	}

	second, err := check(t, gate, "smith", reportOf(
		signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 1),
		signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 2),
		signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, 3),
	))
	if err != nil {
		t.Fatalf("second login: %v", err)
	}
	if second.MachineId != first.MachineId {
		t.Fatal("the same computer under a second account must resolve to the same machine")
	}
	if second.FirstSeen {
		t.Fatal("a machine already known must not be reported as new")
	}
}

func TestSwappedDiskStillMatches(t *testing.T) {
	gate := gateFor(t, `{"mode": "enforce", "requireChallenge": false}`)
	first, err := check(t, gate, "neo", reportOf(
		signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 1),
		signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 2),
		signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, 3),
	))
	if err != nil {
		t.Fatal(err)
	}

	second, err := check(t, gate, "neo", reportOf(
		signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 1),
		signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 9),
		signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, 3),
	))
	if err != nil {
		t.Fatal(err)
	}
	if second.MachineId != first.MachineId {
		t.Fatal("swapping a disk must not create a new machine")
	}
}

func TestWeakSignalsAloneAreNotAMatch(t *testing.T) {
	gate := gateFor(t, `{"mode": "enforce", "requireChallenge": false}`)
	first, err := check(t, gate, "neo", reportOf(
		signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 1),
		signal(apiv1.SignalKind_SIGNAL_KIND_GPU, 2),
		signal(apiv1.SignalKind_SIGNAL_KIND_CPU, 3),
		signal(apiv1.SignalKind_SIGNAL_KIND_MEMORY_SIZE, 4),
	))
	if err != nil {
		t.Fatal(err)
	}

	second, err := check(t, gate, "smith", reportOf(
		signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 7),
		signal(apiv1.SignalKind_SIGNAL_KIND_GPU, 2),
		signal(apiv1.SignalKind_SIGNAL_KIND_CPU, 3),
		signal(apiv1.SignalKind_SIGNAL_KIND_MEMORY_SIZE, 4),
	))
	if err != nil {
		t.Fatal(err)
	}
	if second.MachineId == first.MachineId {
		t.Fatal("a shared GPU, CPU and memory size must never identify a machine")
	}
}

func TestBanFollowsTheMachineToANewAccount(t *testing.T) {
	gate := gateFor(t, `{"mode": "enforce", "requireChallenge": false}`)
	report := func() *apiv1.MachineReport {
		return reportOf(
			signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 1),
			signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 2),
			signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, 3),
		)
	}
	if _, err := check(t, gate, "griefer", report()); err != nil {
		t.Fatal(err)
	}

	outcome, err := gate.Ban(context.Background(), hwid.BanRequest{Subject: "griefer", Username: "griefer", Reason: "griefing", By: "test"})
	if err != nil {
		t.Fatalf("ban: %v", err)
	}
	if outcome.Scope != apiv1.BanScope_BAN_SCOPE_CLUSTER {
		t.Fatalf("a lone untrusted machine should be banned by cluster, got %s", hwid.ScopeWord(outcome.Scope))
	}
	if outcome.Ban.ExpiresAt.IsZero() {
		t.Fatal("hardware bans must expire")
	}

	if _, err := check(t, gate, "griefer", report()); err == nil {
		t.Fatal("the banned account must be refused")
	}
	_, err = check(t, gate, "griefer2", report())
	banErr, ok := hwid.AsBanError(err)
	if !ok {
		t.Fatalf("a new account on the banned machine must be refused, got %v", err)
	}
	if banErr.Ban.Reference != outcome.Ban.Reference {
		t.Fatal("the refusal must name the ban that caused it")
	}

	if err := gate.Unban(context.Background(), outcome.Ban.Reference); err != nil {
		t.Fatal(err)
	}
	if _, err := check(t, gate, "griefer", report()); err != nil {
		t.Fatalf("lifting the ban must let the player back in, got %v", err)
	}
}

func TestObserveModeRecordsButRefusesNobody(t *testing.T) {
	gate := gateFor(t, `{"mode": "observe"}`)
	report := reportOf(
		signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 1),
		signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 2),
		signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, 3),
	)
	if _, err := check(t, gate, "neo", report); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Ban(context.Background(), hwid.BanRequest{Subject: "neo", Username: "neo", Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := check(t, gate, "neo", report); err != nil {
		t.Fatalf("observe mode must never refuse anyone, got %v", err)
	}
}

func TestConfirmationGuardsWideBans(t *testing.T) {
	gate := gateFor(t, `{"mode": "enforce", "requireChallenge": false}`)
	report := func() *apiv1.MachineReport {
		return reportOf(
			signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 1),
			signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 2),
			signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, 3),
		)
	}
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		if _, err := check(t, gate, name, report()); err != nil {
			t.Fatal(err)
		}
	}

	outcome, err := gate.Ban(context.Background(), hwid.BanRequest{Subject: "a", Username: "a", Reason: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.NeedsConfirmation {
		t.Fatal("a ban reaching five accounts must ask before it lands")
	}
	if _, err := check(t, gate, "b", report()); err != nil {
		t.Fatal("nothing may be banned while confirmation is pending")
	}

	confirmed, err := gate.Ban(context.Background(), hwid.BanRequest{Subject: "a", Username: "a", Reason: "test", Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.NeedsConfirmation {
		t.Fatal("--yes must apply the ban")
	}
}

func TestTrustedMachineNarrowsTheBan(t *testing.T) {
	gate := gateFor(t, `{"mode": "enforce", "requireChallenge": false}`)
	verdict, err := check(t, gate, "neo", reportOf(
		signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 1),
		signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 2),
		signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, 3),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.Store().SetTrusted(context.Background(), verdict.MachineId, true); err != nil {
		t.Fatal(err)
	}
	outcome, err := gate.Ban(context.Background(), hwid.BanRequest{Subject: "neo", Username: "neo", Reason: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Scope != apiv1.BanScope_BAN_SCOPE_ACCOUNT {
		t.Fatalf("a trusted machine must never be banned by accident, got %s", hwid.ScopeWord(outcome.Scope))
	}
}

func TestFanOutDemotesUselessDigests(t *testing.T) {
	gate := gateFor(t, `{"mode": "enforce", "requireChallenge": false, "fanOutLimit": 3}`)
	for index := byte(0); index < 6; index++ {
		if _, err := check(t, gate, string(rune('a'+index)), reportOf(
			signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 200),
			signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, index),
			signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, index+100),
		)); err != nil {
			t.Fatal(err)
		}
	}
	fresh, err := check(t, gate, "newcomer", reportOf(
		signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 200),
		signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 77),
		signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, 78),
	))
	if err != nil {
		t.Fatal(err)
	}
	if !fresh.FirstSeen {
		t.Fatal("a digest shared by every machine must stop counting as evidence")
	}
}

func TestChallengeIsSingleUse(t *testing.T) {
	gate := gateFor(t, `{"mode": "enforce"}`)
	nonce, _, err := gate.Challenge()
	if err != nil {
		t.Fatal(err)
	}
	report := reportOf(
		signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 1),
		signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 2),
		signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, 3),
	)
	report.Nonce = nonce
	if _, err := check(t, gate, "neo", report); err != nil {
		t.Fatalf("a fresh challenge must be accepted: %v", err)
	}
	if _, err := check(t, gate, "smith", report); err == nil {
		t.Fatal("a nonce must not be usable twice")
	}
}

func TestSignatureIsVerifiedAgainstTheCanonicalForm(t *testing.T) {
	gate := gateFor(t, `{"mode": "enforce", "requireChallenge": false}`)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	report := reportOf(
		signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 1),
		signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 2),
		signal(apiv1.SignalKind_SIGNAL_KIND_PLATFORM_KEY, 3),
	)
	report.PlatformKeyPublic = hwid.EncodePublicKey(public)
	report.PlatformKeySignature = ed25519.Sign(private, hwid.Canonical(report))

	if _, err := check(t, gate, "neo", report); err != nil {
		t.Fatalf("a correctly signed report must be accepted: %v", err)
	}

	report.Signals[0].Digest = digest(9)
	if _, err := check(t, gate, "neo", report); err == nil {
		t.Fatal("changing a signed field must invalidate the signature")
	}
}

func TestMalformedReportsAreRefused(t *testing.T) {
	gate := gateFor(t, `{"mode": "enforce", "requireChallenge": false}`)
	cases := map[string]*apiv1.MachineReport{
		"no signals":      reportOf(),
		"short digest":    {SchemaVersion: hwid.ReportSchemaVersion, Signals: []*apiv1.Signal{{Kind: apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, Digest: []byte{1}}}},
		"unknown kind":    {SchemaVersion: hwid.ReportSchemaVersion, Signals: []*apiv1.Signal{{Digest: digest(1)}}},
		"wrong schema":    {SchemaVersion: 99, Signals: []*apiv1.Signal{signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, 1)}},
		"stale timestamp": {SchemaVersion: hwid.ReportSchemaVersion, Signals: []*apiv1.Signal{signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, 1)}, CollectedAtUnixNanos: time.Now().Add(-time.Hour).UnixNano()},
	}
	for name, report := range cases {
		if _, err := check(t, gate, "neo", report); err == nil {
			t.Fatalf("%s: expected a refusal", name)
		}
	}
}

func TestVirtualMachinePolicy(t *testing.T) {
	report := func() *apiv1.MachineReport {
		r := reportOf(
			signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 1),
			signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 2),
			signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, 3),
		)
		r.Flags = []apiv1.CollectorFlag{apiv1.CollectorFlag_COLLECTOR_FLAG_VIRTUAL_MACHINE}
		return r
	}
	flagged := gateFor(t, `{"mode": "enforce", "requireChallenge": false, "vmPolicy": "flag"}`)
	if _, err := check(t, flagged, "neo", report()); err != nil {
		t.Fatalf("vmPolicy flag must let virtual machines in: %v", err)
	}
	denied := gateFor(t, `{"mode": "enforce", "requireChallenge": false, "vmPolicy": "deny"}`)
	if _, err := check(t, denied, "neo", report()); err == nil {
		t.Fatal("vmPolicy deny must refuse virtual machines")
	}
}

func TestTicketsAreRequiredOnlyWhenAsked(t *testing.T) {
	neo := hwid.Identity{Subject: "neo"}
	open := gateFor(t, `{"mode": "enforce", "requireChallenge": false}`)
	if err := open.VerifyTicket("", neo); err != nil {
		t.Fatalf("without requireLauncher an absent ticket is fine: %v", err)
	}

	strict := gateFor(t, `{"mode": "enforce", "requireChallenge": false, "requireLauncher": true}`)
	if err := strict.VerifyTicket("", neo); err == nil {
		t.Fatal("requireLauncher must refuse a caller with no ticket")
	}
	verdict, err := check(t, strict, "neo", reportOf(
		signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 1),
		signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 2),
		signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, 3),
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := strict.VerifyTicket(verdict.MachineTicket, neo); err != nil {
		t.Fatalf("the launcher's own ticket must pass: %v", err)
	}
	if err := strict.VerifyTicket(verdict.MachineTicket+"x", neo); err == nil {
		t.Fatal("a tampered ticket must not pass")
	}
	if err := strict.VerifyTicket(verdict.MachineTicket, hwid.Identity{Subject: "trinity"}); err == nil {
		t.Fatal("чужой билет не должен пускать в игру под другим аккаунтом")
	}
}

func TestSQLiteStoreRoundTrip(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "machines.db")
	gate := gateFor(t, `{"mode": "enforce", "requireChallenge": false, "store": {"backend": "sql", "config": {"driver": "sqlite", "dsn": "`+dsn+`"}}}`)
	report := func() *apiv1.MachineReport {
		return reportOf(
			signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 1),
			signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 2),
			signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, 3),
		)
	}
	first, err := check(t, gate, "neo", report())
	if err != nil {
		t.Fatalf("first report: %v", err)
	}
	second, err := check(t, gate, "smith", report())
	if err != nil {
		t.Fatalf("second report: %v", err)
	}
	if second.MachineId != first.MachineId {
		t.Fatal("the sql store must recognise the machine it just recorded")
	}

	accounts, err := gate.Store().AccountsOfMachine(context.Background(), first.MachineId)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 {
		t.Fatalf("expected both accounts on the machine, got %d", len(accounts))
	}

	outcome, err := gate.Ban(context.Background(), hwid.BanRequest{Subject: "neo", Username: "neo", Reason: "test", Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := check(t, gate, "third", report()); err == nil {
		t.Fatal("the ban must be enforced from the sql store")
	}
	stored, err := gate.BanByReference(context.Background(), outcome.Ban.Reference)
	if err != nil || stored == nil {
		t.Fatalf("the ban must be findable by its reference: %v", err)
	}
}

func TestHardwareKeyCanBeRequired(t *testing.T) {
	software := func() *apiv1.MachineReport {
		report := reportOf(
			signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 1),
			signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 2),
			signal(apiv1.SignalKind_SIGNAL_KIND_PLATFORM_KEY, 3),
		)
		report.Flags = []apiv1.CollectorFlag{apiv1.CollectorFlag_COLLECTOR_FLAG_PLATFORM_KEY_FALLBACK}
		return report
	}

	relaxed := gateFor(t, `{"mode": "enforce", "requireChallenge": false}`)
	if _, err := check(t, relaxed, "neo", software()); err != nil {
		t.Fatalf("a software key is fine by default: %v", err)
	}

	strict := gateFor(t, `{"mode": "enforce", "requireChallenge": false, "requireHardwareKey": true}`)
	if _, err := check(t, strict, "neo", software()); !errors.Is(err, hwid.ErrSoftwareKey) {
		t.Fatalf("requireHardwareKey must refuse a software key, got %v", err)
	}

	hardware := reportOf(
		signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 1),
		signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 2),
		signal(apiv1.SignalKind_SIGNAL_KIND_PLATFORM_KEY, 3),
	)
	if _, err := check(t, strict, "neo", hardware); err != nil {
		t.Fatalf("a hardware-backed key must pass: %v", err)
	}
}

func TestEcdsaPlatformKeyIsAccepted(t *testing.T) {
	gate := gateFor(t, `{"mode": "enforce", "requireChallenge": false, "requireHardwareKey": true}`)
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	report := reportOf(
		signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 1),
		signal(apiv1.SignalKind_SIGNAL_KIND_PLATFORM_KEY, 2),
	)
	der, err := x509.MarshalPKIXPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	report.PlatformKeyPublic = der
	sum := sha256.Sum256(hwid.Canonical(report))
	report.PlatformKeySignature, err = ecdsa.SignASN1(rand.Reader, private, sum[:])
	if err != nil {
		t.Fatal(err)
	}

	if _, err := check(t, gate, "neo", report); err != nil {
		t.Fatalf("an ecdsa platform key must verify: %v", err)
	}

	report.Signals[0].Digest = digest(9)
	if _, err := check(t, gate, "neo", report); err == nil {
		t.Fatal("changing a signed field must invalidate the ecdsa signature")
	}
}

func TestSubjectOfMatchesTheCaseTheProviderIssued(t *testing.T) {
	gate := gateFor(t, `{"mode": "enforce", "requireChallenge": false}`)
	ctx := context.Background()
	identity := hwid.Identity{Subject: "eec62300-5d01", Username: "Strah"}
	report := func() *apiv1.MachineReport {
		return reportOf(
			signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 9),
			signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 8),
			signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, 7),
		)
	}
	if _, err := gate.Check(ctx, identity, report(), "127.0.0.1"); err != nil {
		t.Fatal(err)
	}

	for _, typed := range []string{"Strah", "strah", "STRAH"} {
		subject, err := gate.SubjectOf(ctx, typed)
		if err != nil {
			t.Fatal(err)
		}
		if subject != identity.Subject {
			t.Fatalf("typed %q resolved to %q, want %q", typed, subject, identity.Subject)
		}
		machines, err := gate.Store().MachinesOfSubject(ctx, subject)
		if err != nil {
			t.Fatal(err)
		}
		if len(machines) != 1 {
			t.Fatalf("typed %q found %d machines", typed, len(machines))
		}
	}

	subject, err := gate.SubjectOf(ctx, "never-signed-in")
	if err != nil {
		t.Fatal(err)
	}
	if subject != "never-signed-in" {
		t.Fatalf("an unseen player must stay bannable by the name typed, got %q", subject)
	}
}

func TestAccountBanTypedAtTheConsoleStopsTheLogin(t *testing.T) {
	gate := gateFor(t, `{"mode": "enforce", "requireChallenge": false}`)
	ctx := context.Background()
	identity := hwid.Identity{Subject: "eec62300-5d01", Username: "Strah"}
	report := func() *apiv1.MachineReport {
		return reportOf(
			signal(apiv1.SignalKind_SIGNAL_KIND_SMBIOS_UUID, 4),
			signal(apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, 5),
			signal(apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, 6),
		)
	}
	if _, err := gate.Check(ctx, identity, report(), "127.0.0.1"); err != nil {
		t.Fatal(err)
	}

	subject, err := gate.SubjectOf(ctx, "strah")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Ban(ctx, hwid.BanRequest{
		Scope:    apiv1.BanScope_BAN_SCOPE_ACCOUNT,
		Subject:  subject,
		Username: "strah",
		Reason:   "cheating",
		By:       "console",
	}); err != nil {
		t.Fatalf("ban: %v", err)
	}

	if _, err := gate.Check(ctx, identity, report(), "127.0.0.1"); err == nil {
		t.Fatal("a ban typed with different case must still refuse the player")
	}
}
