package hwid

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
)

type Candidate struct {
	Machine Machine
	Matched []Signal
}

type Store interface {
	Candidates(ctx context.Context, digests []string) ([]Candidate, error)
	FanOut(ctx context.Context, digests []string) (map[string]int, error)

	SaveMachine(ctx context.Context, machine Machine, signals []Signal) error
	Machine(ctx context.Context, id string) (*Machine, error)
	MachinesOfCluster(ctx context.Context, clusterID string) ([]Machine, error)
	MachinesOfSubject(ctx context.Context, subject string) ([]Machine, error)
	MoveCluster(ctx context.Context, from, to string) error
	SetTrusted(ctx context.Context, machineID string, trusted bool) error

	SeeAccount(ctx context.Context, machineID string, account Account) error
	AccountsOfMachine(ctx context.Context, machineID string) ([]Account, error)
	AccountsOfCluster(ctx context.Context, clusterID string) ([]Account, error)
	SubjectsOfUsername(ctx context.Context, username string) ([]string, error)

	SaveBan(ctx context.Context, ban Ban) error
	ActiveBans(ctx context.Context, targets []string, now time.Time) ([]Ban, error)
	BanByReference(ctx context.Context, reference string) (*Ban, error)
	ListBans(ctx context.Context, includeInactive bool, now time.Time) ([]Ban, error)
	LiftBan(ctx context.Context, reference string) error

	Prune(ctx context.Context, idleBefore, ipBefore time.Time) (int, error)
	Close() error
}

type StoreFactory func(config json.RawMessage) (Store, error)

var storeFactories = map[string]StoreFactory{}

func RegisterStore(name string, factory StoreFactory) {
	storeFactories[name] = factory
}

func StoreNames() []string {
	names := make([]string, 0, len(storeFactories))
	for name := range storeFactories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func BuildStore(cfg StoreConfig) (Store, error) {
	factory, ok := storeFactories[cfg.Backend]
	if !ok {
		return nil, fmt.Errorf("unknown hwid store %q (have %s)", cfg.Backend, strings.Join(StoreNames(), ", "))
	}
	return factory(cfg.Config)
}

func signalKey(kind apiv1.SignalKind, digest string) string {
	return kind.String() + "\x00" + digest
}
