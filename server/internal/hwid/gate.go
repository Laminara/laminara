package hwid

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
	"github.com/laminara/laminara/server/internal/auth"
)

const (
	digestBytes    = 16
	maxSignals     = 64
	nonceBytes     = 24
	reportSkew     = 10 * time.Minute
	maxChallenges  = 100_000
	referenceChars = "0123456789ABCDEF"
)

var (
	ErrReportRequired = errors.New("this server requires a launcher that reports its machine")
	ErrReportInvalid  = errors.New("machine report is not valid")
	ErrChallengeStale = errors.New("machine report answers no live challenge")
	ErrVirtualMachine = errors.New("this server does not accept virtual machines")
	ErrSoftwareKey    = errors.New("this server requires a computer with a TPM or a Secure Enclave the launcher can use")
)

type BanError struct {
	Ban Ban
}

func (e *BanError) Error() string {
	if e.Ban.Reason == "" {
		return "banned (" + e.Ban.Reference + ")"
	}
	return e.Ban.Reason + " (" + e.Ban.Reference + ")"
}

func AsBanError(err error) (*BanError, bool) {
	var banErr *BanError
	ok := errors.As(err, &banErr)
	return banErr, ok
}

type challenge struct {
	expires time.Time
}

type Gate struct {
	cfg     Config
	store   Store
	tickets *Tickets

	mu         sync.Mutex
	challenges map[string]challenge

	now func() time.Time
}

func New(cfg *Config) (*Gate, error) {
	if cfg == nil {
		return nil, nil
	}
	resolved := cfg.withDefaults()
	if err := resolved.validate(); err != nil {
		return nil, err
	}
	if resolved.Mode == ModeOff {
		return nil, nil
	}
	store, err := BuildStore(resolved.Store)
	if err != nil {
		return nil, err
	}
	secret, err := LoadOrCreateSecret(resolved.TicketSecretPath)
	if err != nil {
		return nil, err
	}
	return &Gate{
		cfg:        resolved,
		store:      store,
		tickets:    NewTickets(secret, resolved.TicketTTL.Duration()),
		challenges: map[string]challenge{},
		now:        time.Now,
	}, nil
}

func (g *Gate) Enabled() bool { return g != nil }

func (g *Gate) Mode() Mode {
	if g == nil {
		return ModeOff
	}
	return g.cfg.Mode
}

func (g *Gate) Enforcing() bool { return g != nil && g.cfg.Mode == ModeEnforce }

func (g *Gate) Store() Store {
	if g == nil {
		return nil
	}
	return g.store
}

func (g *Gate) Config() Config {
	if g == nil {
		return Config{Mode: ModeOff}
	}
	return g.cfg
}

func (g *Gate) RequiresLauncher() bool { return g != nil && g.cfg.RequireLauncher }

func (g *Gate) Challenge() ([]byte, time.Time, error) {
	if g == nil {
		return nil, time.Time{}, nil
	}
	nonce := make([]byte, nonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return nil, time.Time{}, err
	}
	expires := g.now().Add(g.cfg.ChallengeTTL.Duration())

	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.challenges) >= maxChallenges {
		g.expireLocked()
	}
	g.challenges[string(nonce)] = challenge{expires: expires}
	return nonce, expires, nil
}

func (g *Gate) expireLocked() {
	now := g.now()
	for key, entry := range g.challenges {
		if now.After(entry.expires) {
			delete(g.challenges, key)
		}
	}
	if len(g.challenges) < maxChallenges {
		return
	}
	g.challenges = map[string]challenge{}
}

func (g *Gate) takeChallenge(nonce []byte) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	entry, ok := g.challenges[string(nonce)]
	if !ok {
		return false
	}
	delete(g.challenges, string(nonce))
	return g.now().Before(entry.expires)
}

func (g *Gate) validate(report *apiv1.MachineReport) error {
	if report == nil {
		return ErrReportInvalid
	}
	if report.SchemaVersion != ReportSchemaVersion {
		return fmt.Errorf("%w: schema version %d, want %d", ErrReportInvalid, report.SchemaVersion, ReportSchemaVersion)
	}
	if len(report.Signals) == 0 || len(report.Signals) > maxSignals {
		return fmt.Errorf("%w: %d signals", ErrReportInvalid, len(report.Signals))
	}
	for _, signal := range report.Signals {
		if len(signal.Digest) != digestBytes {
			return fmt.Errorf("%w: digest is %d bytes, want %d", ErrReportInvalid, len(signal.Digest), digestBytes)
		}
		if signal.Kind == apiv1.SignalKind_SIGNAL_KIND_UNSPECIFIED {
			return fmt.Errorf("%w: unspecified signal kind", ErrReportInvalid)
		}
	}
	collected := time.Unix(0, report.CollectedAtUnixNanos)
	if report.CollectedAtUnixNanos != 0 {
		if delta := g.now().Sub(collected); delta > reportSkew || delta < -reportSkew {
			return fmt.Errorf("%w: collected %s away from now", ErrReportInvalid, delta.Round(time.Second))
		}
	}
	if len(report.PlatformKeyPublic) > 0 {
		if err := verifySignature(report); err != nil {
			return err
		}
	}
	if *g.cfg.RequireChallenge && !g.takeChallenge(report.Nonce) {
		return ErrChallengeStale
	}
	return nil
}

func verifySignature(report *apiv1.MachineReport) error {
	parsed, err := x509.ParsePKIXPublicKey(report.PlatformKeyPublic)
	if err != nil {
		return fmt.Errorf("%w: platform key is not a valid public key", ErrReportInvalid)
	}
	message := Canonical(report)
	switch key := parsed.(type) {
	case ed25519.PublicKey:
		if !ed25519.Verify(key, message, report.PlatformKeySignature) {
			return fmt.Errorf("%w: platform key signature does not match", ErrReportInvalid)
		}
	case *ecdsa.PublicKey:
		if key.Curve != elliptic.P256() {
			return fmt.Errorf("%w: platform key uses an unsupported curve", ErrReportInvalid)
		}
		sum := sha256.Sum256(message)
		if !ecdsa.VerifyASN1(key, sum[:], report.PlatformKeySignature) {
			return fmt.Errorf("%w: platform key signature does not match", ErrReportInvalid)
		}
	default:
		return fmt.Errorf("%w: platform key is neither ed25519 nor ecdsa", ErrReportInvalid)
	}
	return nil
}

type Identity struct {
	Subject  string
	Username string
}

func IdentityOf(identity auth.Identity) Identity {
	return Identity{Subject: identity.Subject, Username: identity.Username}
}

func (i Identity) key() string {
	if i.Subject != "" {
		return i.Subject
	}
	return strings.ToLower(i.Username)
}

func (g *Gate) SubjectOf(ctx context.Context, player string) (string, error) {
	if g == nil {
		return player, nil
	}
	subjects, err := g.store.SubjectsOfUsername(ctx, player)
	if err != nil {
		return "", err
	}
	if len(subjects) == 0 {
		return player, nil
	}
	return subjects[0], nil
}

func (g *Gate) Check(ctx context.Context, identity Identity, report *apiv1.MachineReport, ip string) (*apiv1.MachineVerdict, error) {
	if g == nil {
		return nil, nil
	}
	if report == nil {
		if g.cfg.RequireReport && g.Enforcing() {
			return nil, ErrReportRequired
		}
		return nil, g.CheckSubject(ctx, identity)
	}
	if err := g.validate(report); err != nil {
		if g.Enforcing() {
			return nil, err
		}
		return nil, nil
	}

	virtual := hasFlag(report.Flags, apiv1.CollectorFlag_COLLECTOR_FLAG_VIRTUAL_MACHINE)
	if virtual && g.cfg.VMPolicy == VMDeny && g.Enforcing() {
		return nil, ErrVirtualMachine
	}
	keyFallback := hasFlag(report.Flags, apiv1.CollectorFlag_COLLECTOR_FLAG_PLATFORM_KEY_FALLBACK)
	if keyFallback && g.cfg.RequireHardwareKey && g.Enforcing() {
		return nil, ErrSoftwareKey
	}

	signals := make([]Signal, 0, len(report.Signals))
	digests := make([]string, 0, len(report.Signals))
	for _, signal := range report.Signals {
		digest := hex.EncodeToString(signal.Digest)
		signals = append(signals, Signal{Kind: signal.Kind, Digest: digest, Confidence: signal.Confidence})
		digests = append(digests, digest)
	}

	candidates, err := g.store.Candidates(ctx, digests)
	if err != nil {
		return nil, err
	}
	fanOut, err := g.store.FanOut(ctx, digests)
	if err != nil {
		return nil, err
	}
	resolution := Resolve(candidates, fanOut, g.cfg, virtual, keyFallback)

	now := g.now()
	machine := Machine{
		ID:        resolution.MachineID,
		ClusterID: resolution.ClusterID,
		Platform:  report.Platform.String(),
		Flags:     flagNames(report.Flags),
		FirstSeen: now,
		LastSeen:  now,
		LastIP:    ip,
	}
	firstSeen := !resolution.SameMachine
	if machine.ID == "" {
		machine.ID = uuid.NewString()
	}
	if machine.ClusterID == "" {
		machine.ClusterID = uuid.NewString()
	}

	for _, cluster := range resolution.MergeClusters {
		if err := g.store.MoveCluster(ctx, cluster, machine.ClusterID); err != nil {
			return nil, err
		}
	}
	if err := g.store.SaveMachine(ctx, machine, signals); err != nil {
		return nil, err
	}
	if err := g.store.SeeAccount(ctx, machine.ID, Account{Subject: identity.Subject, Username: identity.Username, LastSeen: now}); err != nil {
		return nil, err
	}

	ban, err := g.findBan(ctx, identity.key(), machine.ID, machine.ClusterID)
	if err != nil {
		return nil, err
	}
	if ban != nil && g.Enforcing() {
		return nil, &BanError{Ban: *ban}
	}

	ticket, expires := g.tickets.Issue(TicketClaims{Subject: identity.key(), MachineID: machine.ID, ClusterID: machine.ClusterID}, now)
	return &apiv1.MachineVerdict{
		MachineId:              machine.ID,
		FirstSeen:              firstSeen,
		MachineTicket:          ticket,
		TicketExpiresUnixNanos: expires.UnixNano(),
	}, nil
}

func (g *Gate) CheckSubject(ctx context.Context, identity Identity) error {
	if g == nil || !g.Enforcing() {
		return nil
	}
	targets := []string{identity.key()}
	machines, err := g.store.MachinesOfSubject(ctx, identity.key())
	if err != nil {
		return err
	}
	clusters := map[string]struct{}{}
	for _, machine := range machines {
		targets = append(targets, machine.ID)
		clusters[machine.ClusterID] = struct{}{}
	}
	for cluster := range clusters {
		targets = append(targets, cluster)
	}
	bans, err := g.store.ActiveBans(ctx, targets, g.now())
	if err != nil {
		return err
	}
	if len(bans) > 0 {
		return &BanError{Ban: bans[0]}
	}
	return nil
}

func (g *Gate) findBan(ctx context.Context, subject, machineID, clusterID string) (*Ban, error) {
	bans, err := g.store.ActiveBans(ctx, []string{subject, machineID, clusterID}, g.now())
	if err != nil {
		return nil, err
	}
	for _, scope := range []apiv1.BanScope{
		apiv1.BanScope_BAN_SCOPE_ACCOUNT,
		apiv1.BanScope_BAN_SCOPE_MACHINE,
		apiv1.BanScope_BAN_SCOPE_CLUSTER,
	} {
		for i := range bans {
			if bans[i].Scope == scope {
				return &bans[i], nil
			}
		}
	}
	return nil, nil
}

func (g *Gate) VerifyTicket(ticket string, identity Identity) error {
	if g == nil || !g.cfg.RequireLauncher {
		return nil
	}
	if ticket == "" {
		return errors.New("this server accepts in-game login only through its launcher")
	}
	claims, err := g.tickets.Verify(ticket, g.now())
	if err != nil {
		return err
	}
	if claims.Subject != identity.key() {
		return errors.New("this launcher ticket belongs to another account")
	}
	return nil
}

func NewReference() string {
	raw := make([]byte, 2)
	if _, err := rand.Read(raw); err != nil {
		return "LM-0000"
	}
	out := []byte("LM-0000")
	for i, b := range raw {
		out[3+i*2] = referenceChars[b>>4]
		out[4+i*2] = referenceChars[b&0x0f]
	}
	return string(out)
}

func (g *Gate) Close() error {
	if g == nil {
		return nil
	}
	return g.store.Close()
}

func EncodePublicKey(key ed25519.PublicKey) []byte {
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return nil
	}
	return der
}
