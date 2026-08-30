package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	period   = 30 * time.Second
	digits   = 6
	issuer   = "Laminara"
	skewOps  = 1
	codeUses = 2
)

var codeMod int64 = pow10(digits)

func pow10(n int) int64 {
	value := int64(1)
	for i := 0; i < n; i++ {
		value *= 10
	}
	return value
}

func Generate() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

func URI(secret, account string) string {
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", issuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", fmt.Sprintf("%d", digits))
	query.Set("period", fmt.Sprintf("%d", int(period/time.Second)))
	return fmt.Sprintf("otpauth://totp/%s:%s?%s", url.PathEscape(issuer), url.PathEscape(account), query.Encode())
}

func Code(secret string, at time.Time) (string, error) {
	key, err := decode(secret)
	if err != nil {
		return "", err
	}
	return hotp(key, at.Unix()/int64(period/time.Second)), nil
}

func Verify(secret, code string, now time.Time) (int64, bool) {
	key, err := decode(secret)
	if err != nil || len(code) != digits || !onlyDigits(code) {
		return 0, false
	}
	current := now.Unix() / int64(period/time.Second)
	for step := current - skewOps; step <= current+skewOps; step++ {
		if match(key, code, step) {
			return step, true
		}
	}
	return 0, false
}

type Verifier struct {
	mu   sync.Mutex
	last map[string]acceptance
	now  func() time.Time
}

type acceptance struct {
	step int64
	used int
}

func NewVerifier() *Verifier {
	return &Verifier{last: map[string]acceptance{}, now: time.Now}
}

func (v *Verifier) Verify(subject, secret, code string) bool {
	now := v.now()
	step, ok := Verify(secret, code, now)
	if !ok {
		return false
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	for name, last := range v.last {
		if last.step < step-1 {
			delete(v.last, name)
		}
	}
	if last, seen := v.last[subject]; seen {
		if last.step > step {
			return false
		}
		if last.step == step {
			if last.used >= codeUses {
				return false
			}
			v.last[subject] = acceptance{step: step, used: last.used + 1}
			return true
		}
	}
	v.last[subject] = acceptance{step: step, used: 1}
	return true
}

func match(key []byte, code string, step int64) bool {
	return hmac.Equal([]byte(hotp(key, step)), []byte(code))
}

func hotp(key []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", digits, value%uint32(codeMod))
}

func decode(secret string) ([]byte, error) {
	clean := strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '=' {
			return -1
		}
		return unicode.ToUpper(r)
	}, strings.TrimSpace(secret))
	if clean == "" {
		return nil, fmt.Errorf("empty secret")
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(clean)
}

func onlyDigits(code string) bool {
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
