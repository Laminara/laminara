package hwid

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

func init() {
	RegisterStore("memory", func(json.RawMessage) (Store, error) { return NewMemoryStore(), nil })
}

type MemoryStore struct {
	mu       sync.RWMutex
	machines map[string]Machine
	signals  map[string][]Signal
	accounts map[string]map[string]Account
	bans     map[string]Ban
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		machines: map[string]Machine{},
		signals:  map[string][]Signal{},
		accounts: map[string]map[string]Account{},
		bans:     map[string]Ban{},
	}
}

func (s *MemoryStore) Candidates(_ context.Context, digests []string) ([]Candidate, error) {
	wanted := make(map[string]struct{}, len(digests))
	for _, digest := range digests {
		wanted[digest] = struct{}{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	candidates := make([]Candidate, 0)
	for id, machine := range s.machines {
		var matched []Signal
		for _, signal := range s.signals[id] {
			if _, ok := wanted[signal.Digest]; ok {
				matched = append(matched, signal)
			}
		}
		if len(matched) > 0 {
			candidates = append(candidates, Candidate{Machine: machine, Matched: matched})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Machine.ID < candidates[j].Machine.ID })
	return candidates, nil
}

func (s *MemoryStore) FanOut(_ context.Context, digests []string) (map[string]int, error) {
	wanted := make(map[string]struct{}, len(digests))
	for _, digest := range digests {
		wanted[digest] = struct{}{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	counts := map[string]int{}
	for id := range s.machines {
		seen := map[string]struct{}{}
		for _, signal := range s.signals[id] {
			if _, ok := wanted[signal.Digest]; !ok {
				continue
			}
			if _, ok := seen[signal.Digest]; ok {
				continue
			}
			seen[signal.Digest] = struct{}{}
			counts[signal.Digest]++
		}
	}
	return counts, nil
}

func (s *MemoryStore) SaveMachine(_ context.Context, machine Machine, signals []Signal) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.machines[machine.ID]; ok {
		machine.FirstSeen = existing.FirstSeen
		machine.Trusted = existing.Trusted
	}
	s.machines[machine.ID] = machine

	known := map[string]Signal{}
	for _, signal := range s.signals[machine.ID] {
		known[signalKey(signal.Kind, signal.Digest)] = signal
	}
	for _, signal := range signals {
		known[signalKey(signal.Kind, signal.Digest)] = signal
	}
	merged := make([]Signal, 0, len(known))
	for _, signal := range known {
		merged = append(merged, signal)
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Kind != merged[j].Kind {
			return merged[i].Kind < merged[j].Kind
		}
		return merged[i].Digest < merged[j].Digest
	})
	s.signals[machine.ID] = merged
	return nil
}

func (s *MemoryStore) Machine(_ context.Context, id string) (*Machine, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	machine, ok := s.machines[id]
	if !ok {
		return nil, nil
	}
	return &machine, nil
}

func (s *MemoryStore) MachinesOfCluster(_ context.Context, clusterID string) ([]Machine, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Machine
	for _, machine := range s.machines {
		if machine.ClusterID == clusterID {
			out = append(out, machine)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FirstSeen.Before(out[j].FirstSeen) })
	return out, nil
}

func (s *MemoryStore) MachinesOfSubject(_ context.Context, subject string) ([]Machine, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Machine
	for id, accounts := range s.accounts {
		if _, ok := accounts[subject]; !ok {
			continue
		}
		if machine, ok := s.machines[id]; ok {
			out = append(out, machine)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out, nil
}

func (s *MemoryStore) SubjectsOfUsername(_ context.Context, username string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]struct{}{}
	for _, accounts := range s.accounts {
		for _, account := range accounts {
			if strings.EqualFold(account.Username, username) {
				seen[account.Subject] = struct{}{}
			}
		}
	}
	subjects := make([]string, 0, len(seen))
	for subject := range seen {
		subjects = append(subjects, subject)
	}
	sort.Strings(subjects)
	return subjects, nil
}

func (s *MemoryStore) MoveCluster(_ context.Context, from, to string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, machine := range s.machines {
		if machine.ClusterID == from {
			machine.ClusterID = to
			s.machines[id] = machine
		}
	}
	return nil
}

func (s *MemoryStore) SetTrusted(_ context.Context, machineID string, trusted bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	machine, ok := s.machines[machineID]
	if !ok {
		return nil
	}
	machine.Trusted = trusted
	s.machines[machineID] = machine
	return nil
}

func (s *MemoryStore) SeeAccount(_ context.Context, machineID string, account Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accounts[machineID] == nil {
		s.accounts[machineID] = map[string]Account{}
	}
	s.accounts[machineID][account.Subject] = account
	return nil
}

func (s *MemoryStore) AccountsOfMachine(_ context.Context, machineID string) ([]Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedAccounts(s.accounts[machineID]), nil
}

func (s *MemoryStore) AccountsOfCluster(_ context.Context, clusterID string) ([]Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	merged := map[string]Account{}
	for id, machine := range s.machines {
		if machine.ClusterID != clusterID {
			continue
		}
		for subject, account := range s.accounts[id] {
			if existing, ok := merged[subject]; !ok || account.LastSeen.After(existing.LastSeen) {
				merged[subject] = account
			}
		}
	}
	return sortedAccounts(merged), nil
}

func sortedAccounts(accounts map[string]Account) []Account {
	out := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		out = append(out, account)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}

func (s *MemoryStore) SaveBan(_ context.Context, ban Ban) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bans[ban.Reference] = ban
	return nil
}

func (s *MemoryStore) ActiveBans(_ context.Context, targets []string, now time.Time) ([]Ban, error) {
	wanted := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		wanted[target] = struct{}{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Ban
	for _, ban := range s.bans {
		if _, ok := wanted[ban.Target]; !ok {
			continue
		}
		if ban.Active(now) {
			out = append(out, ban)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Scope < out[j].Scope })
	return out, nil
}

func (s *MemoryStore) BanByReference(_ context.Context, reference string) (*Ban, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ban, ok := s.bans[reference]
	if !ok {
		return nil, nil
	}
	return &ban, nil
}

func (s *MemoryStore) ListBans(_ context.Context, includeInactive bool, now time.Time) ([]Ban, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Ban
	for _, ban := range s.bans {
		if includeInactive || ban.Active(now) {
			out = append(out, ban)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *MemoryStore) LiftBan(_ context.Context, reference string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ban, ok := s.bans[reference]
	if !ok {
		return nil
	}
	ban.Lifted = true
	s.bans[reference] = ban
	return nil
}

func (s *MemoryStore) Prune(_ context.Context, idleBefore, ipBefore time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for id, machine := range s.machines {
		if machine.LastSeen.Before(idleBefore) {
			delete(s.machines, id)
			delete(s.signals, id)
			delete(s.accounts, id)
			removed++
			continue
		}
		if machine.LastIP != "" && machine.LastSeen.Before(ipBefore) {
			machine.LastIP = ""
			s.machines[id] = machine
		}
	}
	return removed, nil
}

func (s *MemoryStore) Close() error { return nil }
