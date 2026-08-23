package hwid_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
	"github.com/laminara/laminara/server/internal/hwid"
)

func fixture() *apiv1.MachineReport {
	first := make([]byte, 16)
	for i := range first {
		first[i] = byte(i)
	}
	second := make([]byte, 16)
	for i := range second {
		second[i] = 0xAB
	}
	return &apiv1.MachineReport{
		SchemaVersion: 1,
		Signals: []*apiv1.Signal{
			{Kind: apiv1.SignalKind_SIGNAL_KIND_MACHINE_ID, Digest: first, Confidence: 100},
			{Kind: apiv1.SignalKind_SIGNAL_KIND_DISK_SERIAL, Digest: second, Confidence: 50},
		},
		Flags: []apiv1.CollectorFlag{
			apiv1.CollectorFlag_COLLECTOR_FLAG_VIRTUAL_MACHINE,
			apiv1.CollectorFlag_COLLECTOR_FLAG_PLATFORM_KEY_FALLBACK,
		},
		Platform:             3,
		OsVersion:            "not signed",
		LauncherVersion:      "not signed",
		Nonce:                []byte{0xDE, 0xAD, 0xBE, 0xEF},
		PlatformKeyPublic:    []byte{1, 2, 3},
		PlatformKeySignature: []byte{4, 5, 6},
		CollectedAtUnixNanos: 1735689600000000000,
	}
}

const canonicalFixtureSHA256 = "30ac67a2e51a688728889ee17f40d60240876ab7d5ffc0e184dcd7f745af06de"

func TestCanonicalFormIsStable(t *testing.T) {
	sum := sha256.Sum256(hwid.Canonical(fixture()))
	if got := hex.EncodeToString(sum[:]); got != canonicalFixtureSHA256 {
		t.Fatalf("the signed form changed: %s\nthe launcher signs the old one, so every report would now be rejected", got)
	}
}
