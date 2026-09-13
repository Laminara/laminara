package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/laminara/laminara/server/internal/atomicfile"
	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/auth/hash"
	"github.com/laminara/laminara/server/internal/auth/totp"
)

func init() {
	auth.RegisterProvider("jsonfile", newJSONFile)
}

type jsonFileConfig struct {
	Path   string `json:"path"`
	Hash   string `json:"hash"`
	Fields struct {
		Username  string `json:"username"`
		Password  string `json:"password"`
		UUID      string `json:"uuid"`
		TwoFactor string `json:"twoFactorSecret"`
	} `json:"fields"`
}

type fileStamp struct {
	size    int64
	changed time.Time
}

type jsonFileProvider struct {
	verifier    hash.Verifier
	scheme      string
	path        string
	usernameKey string
	passwordKey string
	uuidKey     string
	secretKey   string
	second      *totp.Verifier

	mu      sync.RWMutex
	records []map[string]any
	users   map[string]map[string]any
	stamp   fileStamp
}

func newJSONFile(raw json.RawMessage) (auth.Provider, error) {
	var cfg jsonFileConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	scheme := orDefault(cfg.Hash, "argon2id")
	verifier, err := verifierFor(scheme)
	if err != nil {
		return nil, err
	}
	provider := &jsonFileProvider{
		verifier:    verifier,
		scheme:      scheme,
		path:        cfg.Path,
		usernameKey: orDefault(cfg.Fields.Username, "username"),
		passwordKey: orDefault(cfg.Fields.Password, "password"),
		uuidKey:     orDefault(cfg.Fields.UUID, "uuid"),
		secretKey:   orDefault(cfg.Fields.TwoFactor, "totp"),
		second:      totp.NewVerifier(),
	}
	if err := provider.load(); err != nil {
		return nil, err
	}
	return provider, nil
}

func stampOf(path string) fileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{size: info.Size(), changed: info.ModTime()}
}

func (p *jsonFileProvider) load() error {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return err
	}
	var records []map[string]any
	if err := json.Unmarshal(data, &records); err != nil {
		return fmt.Errorf("jsonfile auth: %s не разобрать: %w", p.path, err)
	}
	users := make(map[string]map[string]any, len(records))
	for _, record := range records {
		name, _ := record[p.usernameKey].(string)
		if raw, present := record[p.secretKey]; present && raw != nil {
			secret, isString := raw.(string)
			if !isString || strings.TrimSpace(secret) == "" {
				return fmt.Errorf("jsonfile auth: record %q carries %q that is not a base32 two-factor secret", name, p.secretKey)
			}
		}
		if name != "" {
			users[name] = record
		}
	}
	p.mu.Lock()
	p.records = records
	p.users = users
	p.stamp = stampOf(p.path)
	p.mu.Unlock()
	return nil
}

func (p *jsonFileProvider) record(username string) (map[string]any, bool) {
	p.mu.RLock()
	fresh := p.stamp == stampOf(p.path)
	record, ok := p.users[username]
	p.mu.RUnlock()
	if fresh {
		return record, ok
	}
	if err := p.load(); err != nil {
		return record, ok
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	record, ok = p.users[username]
	return record, ok
}

func (p *jsonFileProvider) Authenticate(_ context.Context, creds auth.Credentials) (auth.Identity, error) {
	record, ok := p.record(creds.Username)
	if !ok {
		spendSameTime(p.verifier, creds.Password)
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	stored, _ := record[p.passwordKey].(string)
	valid, err := verify(p.verifier, p.scheme, creds.Password, stored)
	if err != nil {
		return auth.Identity{}, err
	}
	if !valid {
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	if secret, _ := record[p.secretKey].(string); strings.TrimSpace(secret) != "" {
		if creds.TwoFactorCode == "" {
			return auth.Identity{}, auth.ErrTwoFactorRequired
		}
		if !p.second.Verify(creds.Username, secret, creds.TwoFactorCode) {
			return auth.Identity{}, auth.ErrInvalidCredentials
		}
	}
	return auth.Identity{Subject: creds.Username, Username: creds.Username, UUID: identityUUID(record, p.uuidKey, creds.Username)}, nil
}

func identityUUID(record map[string]any, key, username string) uuid.UUID {
	if raw, ok := record[key].(string); ok {
		if parsed, err := uuid.Parse(raw); err == nil {
			return parsed
		}
	}
	return auth.OfflineUUID(username)
}

func (p *jsonFileProvider) hashed(password string) (string, error) {
	return hash.ProduceCost(p.scheme, password, 0)
}

func (p *jsonFileProvider) save() error {
	data, err := json.MarshalIndent(p.records, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(p.path, append(data, '\n'), 0o600); err != nil {
		return err
	}
	p.stamp = stampOf(p.path)
	return nil
}

func (p *jsonFileProvider) Add(_ context.Context, username, password, wanted string) (auth.Identity, error) {
	if err := p.load(); err != nil {
		return auth.Identity{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, taken := p.users[username]; taken {
		return auth.Identity{}, fmt.Errorf("игрок «%s» уже есть — сменить пароль: auth passwd %s", username, username)
	}
	digest, err := hash.ProduceCost(p.scheme, password, 0)
	if err != nil {
		return auth.Identity{}, err
	}
	record := map[string]any{p.usernameKey: username, p.passwordKey: digest}
	id := auth.OfflineUUID(username)
	if strings.TrimSpace(wanted) != "" {
		parsed, err := uuid.Parse(wanted)
		if err != nil {
			return auth.Identity{}, fmt.Errorf("«%s» не похож на uuid: %w", wanted, err)
		}
		id = parsed
	}
	record[p.uuidKey] = id.String()
	p.records = append(p.records, record)
	p.users[username] = record
	if err := p.save(); err != nil {
		return auth.Identity{}, err
	}
	return auth.Identity{Subject: username, Username: username, UUID: id}, nil
}

func (p *jsonFileProvider) SetPassword(_ context.Context, username, password string) error {
	if err := p.load(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	record, ok := p.users[username]
	if !ok {
		return fmt.Errorf("игрока «%s» нет — завести: auth add %s", username, username)
	}
	digest, err := hash.ProduceCost(p.scheme, password, 0)
	if err != nil {
		return err
	}
	record[p.passwordKey] = digest
	return p.save()
}

func (p *jsonFileProvider) Remove(_ context.Context, username string) error {
	if err := p.load(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.users[username]; !ok {
		return fmt.Errorf("игрока «%s» нет", username)
	}
	kept := make([]map[string]any, 0, len(p.records))
	for _, record := range p.records {
		if name, _ := record[p.usernameKey].(string); name == username {
			continue
		}
		kept = append(kept, record)
	}
	p.records = kept
	delete(p.users, username)
	return p.save()
}

func (p *jsonFileProvider) List(_ context.Context) ([]auth.Identity, error) {
	if err := p.load(); err != nil {
		return nil, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	identities := make([]auth.Identity, 0, len(p.records))
	for _, record := range p.records {
		name, _ := record[p.usernameKey].(string)
		if name == "" {
			continue
		}
		identities = append(identities, auth.Identity{
			Subject:  name,
			Username: name,
			UUID:     identityUUID(record, p.uuidKey, name),
		})
	}
	sort.Slice(identities, func(i, j int) bool {
		return strings.ToLower(identities[i].Username) < strings.ToLower(identities[j].Username)
	})
	return identities, nil
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
