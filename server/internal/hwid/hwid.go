package hwid

import (
	"encoding/json"
	"fmt"
	"time"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
)

const ReportSchemaVersion = 1

type Mode string

const (
	ModeOff     Mode = "off"
	ModeObserve Mode = "observe"
	ModeEnforce Mode = "enforce"
)

type VMPolicy string

const (
	VMAllow VMPolicy = "allow"
	VMFlag  VMPolicy = "flag"
	VMDeny  VMPolicy = "deny"
)

type StoreConfig struct {
	Backend string          `json:"backend"`
	Config  json.RawMessage `json:"config"`
}

type Config struct {
	Mode  Mode        `json:"mode"`
	Store StoreConfig `json:"store"`

	RequireReport      bool  `json:"requireReport"`
	RequireChallenge   *bool `json:"requireChallenge"`
	RequireLauncher    bool  `json:"requireLauncher"`
	RequireHardwareKey bool  `json:"requireHardwareKey"`

	MinScore       int `json:"minScore"`
	MinKinds       int `json:"minKinds"`
	ClusterScore   int `json:"clusterScore"`
	FanOutLimit    int `json:"fanOutLimit"`
	MaxClusterSize int `json:"maxClusterSize"`

	VMPolicy VMPolicy `json:"vmPolicy"`

	HardwareBanTTL Duration `json:"hardwareBanTTL"`
	TicketTTL      Duration `json:"ticketTTL"`
	ChallengeTTL   Duration `json:"challengeTTL"`
	Retention      Duration `json:"retention"`
	IPRetention    Duration `json:"ipRetention"`

	TicketSecretPath string `json:"ticketSecretPath"`
	SaltPath         string `json:"saltPath"`
}

type Duration time.Duration

func (d Duration) Duration() time.Duration { return time.Duration(d) }

func (d *Duration) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (c Config) withDefaults() Config {
	if c.Mode == "" {
		c.Mode = ModeObserve
	}
	if c.Store.Backend == "" {
		c.Store.Backend = "memory"
	}
	if c.MinScore == 0 {
		c.MinScore = 45
	}
	if c.MinKinds == 0 {
		c.MinKinds = 2
	}
	if c.ClusterScore == 0 {
		c.ClusterScore = 25
	}
	if c.FanOutLimit == 0 {
		c.FanOutLimit = 25
	}
	if c.MaxClusterSize == 0 {
		c.MaxClusterSize = 8
	}
	if c.VMPolicy == "" {
		c.VMPolicy = VMFlag
	}
	if c.HardwareBanTTL == 0 {
		c.HardwareBanTTL = Duration(90 * 24 * time.Hour)
	}
	if c.TicketTTL == 0 {
		c.TicketTTL = Duration(10 * time.Minute)
	}
	if c.ChallengeTTL == 0 {
		c.ChallengeTTL = Duration(2 * time.Minute)
	}
	if c.Retention == 0 {
		c.Retention = Duration(180 * 24 * time.Hour)
	}
	if c.IPRetention == 0 {
		c.IPRetention = Duration(30 * 24 * time.Hour)
	}
	if c.RequireChallenge == nil {
		enforce := c.Mode == ModeEnforce
		c.RequireChallenge = &enforce
	}
	return c
}

func (c Config) validate() error {
	switch c.Mode {
	case ModeOff, ModeObserve, ModeEnforce:
	default:
		return fmt.Errorf("hwid.mode %q (want off, observe or enforce)", c.Mode)
	}
	switch c.VMPolicy {
	case VMAllow, VMFlag, VMDeny:
	default:
		return fmt.Errorf("hwid.vmPolicy %q (want allow, flag or deny)", c.VMPolicy)
	}
	if c.MinScore < c.ClusterScore {
		return fmt.Errorf("hwid.minScore (%d) must not be below hwid.clusterScore (%d)", c.MinScore, c.ClusterScore)
	}
	return nil
}

type Signal struct {
	Kind       apiv1.SignalKind
	Digest     string
	Confidence uint32
}

type Machine struct {
	ID        string
	ClusterID string
	Platform  string
	Flags     []string
	Trusted   bool
	FirstSeen time.Time
	LastSeen  time.Time
	LastIP    string
}

type Ban struct {
	Reference string
	Scope     apiv1.BanScope
	Target    string
	Reason    string
	By        string
	CreatedAt time.Time
	ExpiresAt time.Time
	Lifted    bool
}

func (b *Ban) Active(now time.Time) bool {
	return b != nil && !b.Lifted && (b.ExpiresAt.IsZero() || now.Before(b.ExpiresAt))
}

func (b *Ban) Info() *apiv1.BanInfo {
	info := &apiv1.BanInfo{Reason: b.Reason, Reference: b.Reference, Scope: b.Scope}
	if !b.ExpiresAt.IsZero() {
		info.ExpiresUnixNanos = b.ExpiresAt.UnixNano()
	}
	return info
}

type Account struct {
	Subject  string
	Username string
	LastSeen time.Time
}

func flagNames(flags []apiv1.CollectorFlag) []string {
	names := make([]string, 0, len(flags))
	for _, flag := range flags {
		if flag == apiv1.CollectorFlag_COLLECTOR_FLAG_UNSPECIFIED {
			continue
		}
		names = append(names, flag.String())
	}
	return names
}

func hasFlag(flags []apiv1.CollectorFlag, want apiv1.CollectorFlag) bool {
	for _, flag := range flags {
		if flag == want {
			return true
		}
	}
	return false
}

func ScopeWord(scope apiv1.BanScope) string {
	switch scope {
	case apiv1.BanScope_BAN_SCOPE_ACCOUNT:
		return "аккаунт"
	case apiv1.BanScope_BAN_SCOPE_MACHINE:
		return "компьютер"
	case apiv1.BanScope_BAN_SCOPE_CLUSTER:
		return "кластер"
	default:
		return "неизвестно"
	}
}
