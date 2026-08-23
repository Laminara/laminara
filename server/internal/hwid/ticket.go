package hwid

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Tickets struct {
	secret []byte
	ttl    time.Duration
}

func NewTickets(secret []byte, ttl time.Duration) *Tickets {
	return &Tickets{secret: secret, ttl: ttl}
}

func LoadOrCreateSecret(path string) ([]byte, error) {
	if path == "" {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, err
		}
		return secret, nil
	}
	data, err := os.ReadFile(path)
	if err == nil && len(data) >= 32 {
		return data[:32], nil
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, secret, 0o600); err != nil {
		return nil, err
	}
	return secret, nil
}

type TicketClaims struct {
	Subject   string
	MachineID string
	ClusterID string
	Expires   time.Time
}

func (t *Tickets) Issue(claims TicketClaims, now time.Time) (string, time.Time) {
	expires := now.Add(t.ttl)
	payload := strings.Join([]string{
		claims.Subject,
		claims.MachineID,
		claims.ClusterID,
		strconv.FormatInt(expires.UnixNano(), 10),
	}, "\x00")
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encoded + "." + t.sign(encoded), expires
}

func (t *Tickets) sign(encoded string) string {
	mac := hmac.New(sha256.New, t.secret)
	mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (t *Tickets) Verify(ticket string, now time.Time) (TicketClaims, error) {
	encoded, signature, ok := strings.Cut(ticket, ".")
	if !ok {
		return TicketClaims{}, fmt.Errorf("malformed machine ticket")
	}
	if !hmac.Equal([]byte(signature), []byte(t.sign(encoded))) {
		return TicketClaims{}, fmt.Errorf("machine ticket signature does not match")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return TicketClaims{}, fmt.Errorf("malformed machine ticket")
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) != 4 {
		return TicketClaims{}, fmt.Errorf("malformed machine ticket")
	}
	expiresNanos, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return TicketClaims{}, fmt.Errorf("malformed machine ticket")
	}
	expires := time.Unix(0, expiresNanos)
	if now.After(expires) {
		return TicketClaims{}, fmt.Errorf("machine ticket expired")
	}
	return TicketClaims{Subject: parts[0], MachineID: parts[1], ClusterID: parts[2], Expires: expires}, nil
}

func LoadOrCreateSalt(path string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("hwid.saltPath is required once hwid.mode is on")
	}
	return LoadOrCreateSecret(path)
}
