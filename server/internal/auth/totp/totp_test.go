package totp

import (
	"strings"
	"testing"
	"time"
)

func rfcSecret() string {
	return "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
}

func rfcVectors() map[int64]string {
	return map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	}
}

func codeAt(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	code, err := Code(secret, at)
	if err != nil {
		t.Fatalf("код не считается: %v", err)
	}
	return code
}

func TestVerifyAcceptsRfc6238Vectors(t *testing.T) {
	for unix, want := range rfcVectors() {
		at := time.Unix(unix, 0)
		if _, ok := Verify(rfcSecret(), want, at); !ok {
			t.Fatalf("at %d: код %s должен подойти", unix, want)
		}
	}
}

func TestVerifyRejectsWrongCode(t *testing.T) {
	at := time.Unix(59, 0)
	if _, ok := Verify(rfcSecret(), "000000", at); ok {
		t.Fatal("чужой код не должен подходить")
	}
}

func TestVerifyWindowIsOneStepEachWay(t *testing.T) {
	at := time.Unix(59, 0)
	for _, delta := range []int64{-30, 30} {
		if _, ok := Verify(rfcSecret(), "287082", at.Add(time.Duration(delta)*time.Second)); !ok {
			t.Fatalf("код с соседнего шага (%d c) должен подойти", delta)
		}
	}
	if _, ok := Verify(rfcSecret(), "287082", at.Add(60*time.Second)); ok {
		t.Fatal("за пределами окна ±1 шаг код не должен подходить")
	}
}

func TestVerifyToleratesSecretSpacingAndCase(t *testing.T) {
	at := time.Unix(59, 0)
	code := "287082"
	variants := map[string]string{
		"нижний регистр": strings.ToLower(rfcSecret()),
		"пробелы":        strings.Join(strings.Split(rfcSecret(), ""), " "),
		"дефисы":         strings.Join(strings.Split(rfcSecret(), ""), "-"),
		"padding":        rfcSecret() + strings.Repeat("=", 8),
	}
	for name, secret := range variants {
		if _, ok := Verify(secret, code, at); !ok {
			t.Fatalf("секрет (%s) должен работать", name)
		}
	}
}

func TestVerifierAcceptsSameCodeTwice(t *testing.T) {
	verifier := NewVerifier()
	at := time.Unix(59, 0)
	verifier.now = func() time.Time { return at }
	code := codeAt(t, rfcSecret(), at)
	if !verifier.Verify("neo", rfcSecret(), code) {
		t.Fatal("первое использование должно пройти")
	}
	if !verifier.Verify("neo", rfcSecret(), code) {
		t.Fatal("второе использование должно пройти: лаунчер проверяет код в двух шагах входа")
	}
	if verifier.Verify("neo", rfcSecret(), code) {
		t.Fatal("третье использование того же кода должно быть отклонено")
	}
}

func TestVerifierPerSubjectAndNextStep(t *testing.T) {
	verifier := NewVerifier()
	at := time.Unix(59, 0)
	verifier.now = func() time.Time { return at }
	code := codeAt(t, rfcSecret(), at)
	if !verifier.Verify("neo", rfcSecret(), code) || !verifier.Verify("neo", rfcSecret(), code) {
		t.Fatal("два первых использования должны пройти")
	}
	if !verifier.Verify("trinity", rfcSecret(), code) {
		t.Fatal("у другого игрока свой счётчик использований")
	}
	next := at.Add(30 * time.Second)
	verifier.now = func() time.Time { return next }
	if !verifier.Verify("neo", rfcSecret(), codeAt(t, rfcSecret(), next)) {
		t.Fatal("код следующего шага должен пройти после исчерпанного кода")
	}
}

func TestVerifierAllowsNextStepAfterReplay(t *testing.T) {
	verifier := NewVerifier()
	first := time.Unix(59, 0)
	verifier.now = func() time.Time { return first }
	if !verifier.Verify("neo", rfcSecret(), codeAt(t, rfcSecret(), first)) {
		t.Fatal("первый код должен пройти")
	}
	next := first.Add(30 * time.Second)
	verifier.now = func() time.Time { return next }
	if !verifier.Verify("neo", rfcSecret(), codeAt(t, rfcSecret(), next)) {
		t.Fatal("следующий шаг должен пройти после предыдущего")
	}
}

func TestVerifierRejectsGarbage(t *testing.T) {
	verifier := NewVerifier()
	for _, code := range []string{"", "12345", "abcdef", "1234567", "28708O"} {
		if verifier.Verify("neo", rfcSecret(), code) {
			t.Fatalf("код %q не должен проходить", code)
		}
	}
	if verifier.Verify("neo", "не base32!", "287082") {
		t.Fatal("битый секрет не должен пропускать код")
	}
}

func TestGenerateProducesWorkingSecret(t *testing.T) {
	secret, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) != 32 {
		t.Fatalf("секрет из 20 байт должен занимать 32 символа base32, получено %d", len(secret))
	}
	at := time.Unix(59, 0)
	if _, ok := Verify(secret, codeAt(t, secret, at), at); !ok {
		t.Fatal("сгенерированный секрет должен работать")
	}
}

func TestURICarriesStandardParameters(t *testing.T) {
	uri := URI("JBSWY3DPEHPK3PXP", "neo")
	for _, want := range []string{
		"otpauth://totp/Laminara:neo",
		"secret=JBSWY3DPEHPK3PXP",
		"issuer=Laminara",
		"algorithm=SHA1",
		"digits=6",
		"period=30",
	} {
		if !strings.Contains(uri, want) {
			t.Fatalf("в URI %s нет %s", uri, want)
		}
	}
}
