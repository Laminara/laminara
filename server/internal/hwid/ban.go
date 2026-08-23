package hwid

import (
	"context"
	"errors"
	"fmt"
	"time"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
)

const confirmThreshold = 3

type BanRequest struct {
	Scope     apiv1.BanScope
	Subject   string
	Username  string
	MachineID string
	ClusterID string
	Reason    string
	By        string
	TTL       time.Duration
	Permanent bool
	Confirm   bool
}

type BanOutcome struct {
	Ban               Ban
	Scope             apiv1.BanScope
	Accounts          []Account
	Machines          int
	NeedsConfirmation bool
}

var ErrNothingToBan = errors.New("nothing to ban: no machine has been seen for this account")

func (g *Gate) Ban(ctx context.Context, req BanRequest) (BanOutcome, error) {
	if g == nil {
		return BanOutcome{}, errors.New("machine recognition is off")
	}
	now := g.now()
	identity := Identity{Subject: req.Subject, Username: req.Username}
	subject := identity.key()

	machines, err := g.store.MachinesOfSubject(ctx, subject)
	if err != nil {
		return BanOutcome{}, err
	}

	scope := req.Scope
	target := ""
	switch {
	case req.MachineID != "":
		scope, target = apiv1.BanScope_BAN_SCOPE_MACHINE, req.MachineID
	case req.ClusterID != "":
		scope, target = apiv1.BanScope_BAN_SCOPE_CLUSTER, req.ClusterID
	case scope == apiv1.BanScope_BAN_SCOPE_UNSPECIFIED:
		scope, target = g.autoScope(ctx, subject, machines)
	default:
		target, err = g.targetFor(scope, subject, machines)
		if err != nil {
			return BanOutcome{}, err
		}
	}
	if target == "" {
		return BanOutcome{}, ErrNothingToBan
	}

	accounts, machineCount, err := g.reach(ctx, scope, target)
	if err != nil {
		return BanOutcome{}, err
	}
	if len(accounts) > confirmThreshold && !req.Confirm {
		return BanOutcome{Scope: scope, Accounts: accounts, Machines: machineCount, NeedsConfirmation: true}, nil
	}

	ban := Ban{
		Reference: NewReference(),
		Scope:     scope,
		Target:    target,
		Reason:    req.Reason,
		By:        req.By,
		CreatedAt: now,
	}
	switch {
	case req.Permanent:
	case req.TTL > 0:
		ban.ExpiresAt = now.Add(req.TTL)
	case scope != apiv1.BanScope_BAN_SCOPE_ACCOUNT:
		ban.ExpiresAt = now.Add(g.cfg.HardwareBanTTL.Duration())
	}

	if err := g.store.SaveBan(ctx, ban); err != nil {
		return BanOutcome{}, err
	}
	return BanOutcome{Ban: ban, Scope: scope, Accounts: accounts, Machines: machineCount}, nil
}

func (g *Gate) targetFor(scope apiv1.BanScope, subject string, machines []Machine) (string, error) {
	switch scope {
	case apiv1.BanScope_BAN_SCOPE_ACCOUNT:
		return subject, nil
	case apiv1.BanScope_BAN_SCOPE_MACHINE:
		if len(machines) == 0 {
			return "", ErrNothingToBan
		}
		return machines[0].ID, nil
	case apiv1.BanScope_BAN_SCOPE_CLUSTER:
		if len(machines) == 0 {
			return "", ErrNothingToBan
		}
		return machines[0].ClusterID, nil
	default:
		return "", fmt.Errorf("unknown ban scope")
	}
}

func (g *Gate) autoScope(ctx context.Context, subject string, machines []Machine) (apiv1.BanScope, string) {
	if len(machines) == 0 {
		return apiv1.BanScope_BAN_SCOPE_ACCOUNT, subject
	}
	cluster := machines[0].ClusterID
	for _, machine := range machines {
		if machine.ClusterID != cluster || machine.Trusted {
			return apiv1.BanScope_BAN_SCOPE_ACCOUNT, subject
		}
	}
	members, err := g.store.MachinesOfCluster(ctx, cluster)
	if err != nil || len(members) > g.cfg.MaxClusterSize {
		return apiv1.BanScope_BAN_SCOPE_ACCOUNT, subject
	}
	for _, member := range members {
		if member.Trusted {
			return apiv1.BanScope_BAN_SCOPE_ACCOUNT, subject
		}
	}
	return apiv1.BanScope_BAN_SCOPE_CLUSTER, cluster
}

func (g *Gate) reach(ctx context.Context, scope apiv1.BanScope, target string) ([]Account, int, error) {
	switch scope {
	case apiv1.BanScope_BAN_SCOPE_MACHINE:
		accounts, err := g.store.AccountsOfMachine(ctx, target)
		return accounts, 1, err
	case apiv1.BanScope_BAN_SCOPE_CLUSTER:
		accounts, err := g.store.AccountsOfCluster(ctx, target)
		if err != nil {
			return nil, 0, err
		}
		machines, err := g.store.MachinesOfCluster(ctx, target)
		return accounts, len(machines), err
	default:
		return nil, 0, nil
	}
}

func (g *Gate) Unban(ctx context.Context, reference string) error {
	if g == nil {
		return errors.New("machine recognition is off")
	}
	ban, err := g.store.BanByReference(ctx, reference)
	if err != nil {
		return err
	}
	if ban == nil {
		return fmt.Errorf("no ban with reference %s", reference)
	}
	return g.store.LiftBan(ctx, reference)
}

func (g *Gate) Bans(ctx context.Context, includeInactive bool) ([]Ban, error) {
	if g == nil {
		return nil, errors.New("machine recognition is off")
	}
	return g.store.ListBans(ctx, includeInactive, g.now())
}

func (g *Gate) BanByReference(ctx context.Context, reference string) (*Ban, error) {
	if g == nil {
		return nil, errors.New("machine recognition is off")
	}
	return g.store.BanByReference(ctx, reference)
}

func (g *Gate) Prune(ctx context.Context) (int, error) {
	if g == nil {
		return 0, nil
	}
	now := g.now()
	return g.store.Prune(ctx, now.Add(-g.cfg.Retention.Duration()), now.Add(-g.cfg.IPRetention.Duration()))
}
