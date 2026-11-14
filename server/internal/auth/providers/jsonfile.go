package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"sort"
	"strings"
	"sync"

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
			for existing := range users {
				if strings.EqualFold(existing, name) {
					return fmt.Errorf("имя аккаунта %q встречается несколько раз без учёта регистра", name)
				}
			}
			if raw, present := record[p.uuidKey]; present && raw != nil {
				text, ok := raw.(string)
				if !ok {
					return fmt.Errorf("UUID аккаунта %q должен быть строкой", name)
				}
				if _, err := accountUUID(text, name); err != nil {
					return err
				}
			}
			users[name] = record
		}
	}
	p.mu.Lock()
	p.records = records
	p.users = users
	p.mu.Unlock()
	return nil
}

func (p *jsonFileProvider) record(username string) (map[string]any, bool, error) {
	if err := p.load(); err != nil {
		return nil, false, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	for name, record := range p.users {
		if strings.EqualFold(name, username) {
			return maps.Clone(record), true, nil
		}
	}
	return nil, false, nil
}

func (p *jsonFileProvider) Authenticate(_ context.Context, creds auth.Credentials) (auth.Identity, error) {
	record, ok, err := p.record(creds.Username)
	if err != nil {
		return auth.Identity{}, err
	}
	if !ok {
		spendSameTime(p.verifier, creds.Password)
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	creds.Username, _ = record[p.usernameKey].(string)
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
	raw, _ := record[key].(string)
	id, _ := accountUUID(raw, username)
	return id
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
	return nil
}

func (p *jsonFileProvider) Add(_ context.Context, username, password, wanted string) (auth.Identity, error) {
	if err := p.load(); err != nil {
		return auth.Identity{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for name := range p.users {
		if strings.EqualFold(name, username) {
			return auth.Identity{}, fmt.Errorf("игрок «%s» уже есть — сменить пароль: auth passwd %s", username, username)
		}
	}
	digest, err := hash.ProduceCost(p.scheme, password, 0)
	if err != nil {
		return auth.Identity{}, err
	}
	record := map[string]any{p.usernameKey: username, p.passwordKey: digest}
	id, err := accountUUID(strings.TrimSpace(wanted), username)
	if err != nil {
		return auth.Identity{}, err
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

func (p *jsonFileProvider) LookupProfile(_ context.Context, username string) (auth.Identity, error) {
	record, ok, err := p.record(username)
	if err != nil {
		return auth.Identity{}, err
	}
	if !ok {
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	name, _ := record[p.usernameKey].(string)
	return auth.Identity{Subject: name, Username: name, UUID: identityUUID(record, p.uuidKey, name)}, nil
}
