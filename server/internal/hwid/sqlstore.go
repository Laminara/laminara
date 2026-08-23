package hwid

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
	"github.com/laminara/laminara/server/internal/store"
)

func init() {
	RegisterStore("sql", newSQLStore)
}

type sqlStoreConfig struct {
	Driver string `json:"driver"`
	DSN    string `json:"dsn"`
}

type machineRow struct {
	bun.BaseModel `bun:"table:hwid_machine"`

	ID        string    `bun:"id,pk"`
	ClusterID string    `bun:"cluster_id,notnull"`
	Platform  string    `bun:"platform"`
	Flags     string    `bun:"flags"`
	Trusted   bool      `bun:"trusted"`
	FirstSeen time.Time `bun:"first_seen,notnull"`
	LastSeen  time.Time `bun:"last_seen,notnull"`
	LastIP    string    `bun:"last_ip"`
}

type signalRow struct {
	bun.BaseModel `bun:"table:hwid_signal"`

	MachineID  string `bun:"machine_id,pk"`
	Kind       int32  `bun:"kind,pk"`
	Digest     string `bun:"digest,pk"`
	Confidence uint32 `bun:"confidence"`
}

type accountRow struct {
	bun.BaseModel `bun:"table:hwid_account"`

	MachineID string    `bun:"machine_id,pk"`
	Subject   string    `bun:"subject,pk"`
	Username  string    `bun:"username"`
	LastSeen  time.Time `bun:"last_seen,notnull"`
}

type banRow struct {
	bun.BaseModel `bun:"table:hwid_ban"`

	Reference string    `bun:"reference,pk"`
	Scope     int32     `bun:"scope,notnull"`
	Target    string    `bun:"target,notnull"`
	Reason    string    `bun:"reason"`
	By        string    `bun:"issued_by"`
	CreatedAt time.Time `bun:"created_at,notnull"`
	ExpiresAt time.Time `bun:"expires_at,nullzero"`
	Lifted    bool      `bun:"lifted"`
}

type SQLStore struct {
	db *bun.DB
}

func newSQLStore(raw json.RawMessage) (Store, error) {
	var cfg sqlStoreConfig
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, err
		}
	}
	if cfg.Driver == "" {
		cfg.Driver = string(store.DriverSQLite)
	}
	if cfg.DSN == "" {
		return nil, fmt.Errorf("hwid sql store needs a dsn")
	}
	db, err := store.Open(store.Config{Driver: store.Driver(cfg.Driver), DSN: cfg.DSN})
	if err != nil {
		return nil, err
	}
	s := &SQLStore{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLStore) migrate(ctx context.Context) error {
	models := []any{(*machineRow)(nil), (*signalRow)(nil), (*accountRow)(nil), (*banRow)(nil)}
	for _, model := range models {
		if _, err := s.db.NewCreateTable().Model(model).IfNotExists().Exec(ctx); err != nil {
			return err
		}
	}
	indexes := []struct{ name, table, columns string }{
		{"hwid_signal_lookup", "hwid_signal", "digest"},
		{"hwid_machine_cluster", "hwid_machine", "cluster_id"},
		{"hwid_account_subject", "hwid_account", "subject"},
		{"hwid_ban_target", "hwid_ban", "target"},
	}
	for _, index := range indexes {
		query := fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (%s)", index.name, index.table, index.columns)
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStore) Candidates(ctx context.Context, digests []string) ([]Candidate, error) {
	if len(digests) == 0 {
		return nil, nil
	}
	var rows []signalRow
	err := s.db.NewSelect().Model(&rows).Where("digest IN (?)", bun.In(digests)).Scan(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	byMachine := map[string][]Signal{}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if _, ok := byMachine[row.MachineID]; !ok {
			ids = append(ids, row.MachineID)
		}
		byMachine[row.MachineID] = append(byMachine[row.MachineID], Signal{
			Kind:       apiv1.SignalKind(row.Kind),
			Digest:     row.Digest,
			Confidence: row.Confidence,
		})
	}

	var machines []machineRow
	if err := s.db.NewSelect().Model(&machines).Where("id IN (?)", bun.In(ids)).Scan(ctx); err != nil {
		return nil, err
	}
	candidates := make([]Candidate, 0, len(machines))
	for _, row := range machines {
		candidates = append(candidates, Candidate{Machine: row.toMachine(), Matched: byMachine[row.ID]})
	}
	return candidates, nil
}

func (s *SQLStore) FanOut(ctx context.Context, digests []string) (map[string]int, error) {
	if len(digests) == 0 {
		return map[string]int{}, nil
	}
	var rows []struct {
		Digest string `bun:"digest"`
		Total  int    `bun:"total"`
	}
	err := s.db.NewSelect().
		Model((*signalRow)(nil)).
		Column("digest").
		ColumnExpr("COUNT(DISTINCT machine_id) AS total").
		Where("digest IN (?)", bun.In(digests)).
		GroupExpr("digest").
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.Digest] = row.Total
	}
	return counts, nil
}

func (s *SQLStore) SaveMachine(ctx context.Context, machine Machine, signals []Signal) error {
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var existing machineRow
		err := tx.NewSelect().Model(&existing).Where("id = ?", machine.ID).Scan(ctx)
		row := machineRow{
			ID:        machine.ID,
			ClusterID: machine.ClusterID,
			Platform:  machine.Platform,
			Flags:     strings.Join(machine.Flags, ","),
			Trusted:   machine.Trusted,
			FirstSeen: machine.FirstSeen,
			LastSeen:  machine.LastSeen,
			LastIP:    machine.LastIP,
		}
		if err == nil {
			row.FirstSeen = existing.FirstSeen
			row.Trusted = existing.Trusted
			if _, err := tx.NewUpdate().Model(&row).WherePK().Exec(ctx); err != nil {
				return err
			}
		} else {
			if _, err := tx.NewInsert().Model(&row).Exec(ctx); err != nil {
				return err
			}
		}
		for _, signal := range signals {
			signalRecord := signalRow{
				MachineID:  machine.ID,
				Kind:       int32(signal.Kind),
				Digest:     signal.Digest,
				Confidence: signal.Confidence,
			}
			if _, err := tx.NewInsert().Model(&signalRecord).Ignore().Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLStore) Machine(ctx context.Context, id string) (*Machine, error) {
	var row machineRow
	if err := s.db.NewSelect().Model(&row).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, nil
	}
	machine := row.toMachine()
	return &machine, nil
}

func (s *SQLStore) MachinesOfCluster(ctx context.Context, clusterID string) ([]Machine, error) {
	var rows []machineRow
	if err := s.db.NewSelect().Model(&rows).Where("cluster_id = ?", clusterID).Order("first_seen ASC").Scan(ctx); err != nil {
		return nil, err
	}
	return toMachines(rows), nil
}

func (s *SQLStore) MachinesOfSubject(ctx context.Context, subject string) ([]Machine, error) {
	var rows []machineRow
	err := s.db.NewSelect().
		Model(&rows).
		Where("id IN (SELECT machine_id FROM hwid_account WHERE subject = ?)", subject).
		Order("last_seen DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return toMachines(rows), nil
}

func (s *SQLStore) SubjectsOfUsername(ctx context.Context, username string) ([]string, error) {
	var subjects []string
	err := s.db.NewSelect().
		Model((*accountRow)(nil)).
		Column("subject").
		Distinct().
		Where("LOWER(username) = LOWER(?)", username).
		Order("subject").
		Scan(ctx, &subjects)
	if err != nil {
		return nil, err
	}
	return subjects, nil
}

func (s *SQLStore) MoveCluster(ctx context.Context, from, to string) error {
	_, err := s.db.NewUpdate().Model((*machineRow)(nil)).Set("cluster_id = ?", to).Where("cluster_id = ?", from).Exec(ctx)
	return err
}

func (s *SQLStore) SetTrusted(ctx context.Context, machineID string, trusted bool) error {
	_, err := s.db.NewUpdate().Model((*machineRow)(nil)).Set("trusted = ?", trusted).Where("id = ?", machineID).Exec(ctx)
	return err
}

func (s *SQLStore) SeeAccount(ctx context.Context, machineID string, account Account) error {
	row := accountRow{MachineID: machineID, Subject: account.Subject, Username: account.Username, LastSeen: account.LastSeen}
	_, err := s.db.NewInsert().Model(&row).
		On("CONFLICT (machine_id, subject) DO UPDATE").
		Set("username = EXCLUDED.username").
		Set("last_seen = EXCLUDED.last_seen").
		Exec(ctx)
	return err
}

func (s *SQLStore) AccountsOfMachine(ctx context.Context, machineID string) ([]Account, error) {
	var rows []accountRow
	if err := s.db.NewSelect().Model(&rows).Where("machine_id = ?", machineID).Order("last_seen DESC").Scan(ctx); err != nil {
		return nil, err
	}
	return toAccounts(rows), nil
}

func (s *SQLStore) AccountsOfCluster(ctx context.Context, clusterID string) ([]Account, error) {
	var rows []accountRow
	err := s.db.NewSelect().
		Model(&rows).
		Where("machine_id IN (SELECT id FROM hwid_machine WHERE cluster_id = ?)", clusterID).
		Order("last_seen DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	unique := make([]accountRow, 0, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.Subject]; ok {
			continue
		}
		seen[row.Subject] = struct{}{}
		unique = append(unique, row)
	}
	return toAccounts(unique), nil
}

func (s *SQLStore) SaveBan(ctx context.Context, ban Ban) error {
	row := banRow{
		Reference: ban.Reference,
		Scope:     int32(ban.Scope),
		Target:    ban.Target,
		Reason:    ban.Reason,
		By:        ban.By,
		CreatedAt: ban.CreatedAt,
		ExpiresAt: ban.ExpiresAt,
		Lifted:    ban.Lifted,
	}
	_, err := s.db.NewInsert().Model(&row).
		On("CONFLICT (reference) DO UPDATE").
		Set("lifted = EXCLUDED.lifted").
		Set("reason = EXCLUDED.reason").
		Set("expires_at = EXCLUDED.expires_at").
		Exec(ctx)
	return err
}

func (s *SQLStore) ActiveBans(ctx context.Context, targets []string, now time.Time) ([]Ban, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	var rows []banRow
	err := s.db.NewSelect().
		Model(&rows).
		Where("target IN (?)", bun.In(targets)).
		Where("lifted = ?", false).
		Where("expires_at IS NULL OR expires_at > ?", now).
		Order("scope ASC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return toBans(rows), nil
}

func (s *SQLStore) BanByReference(ctx context.Context, reference string) (*Ban, error) {
	var row banRow
	if err := s.db.NewSelect().Model(&row).Where("reference = ?", reference).Scan(ctx); err != nil {
		return nil, nil
	}
	ban := row.toBan()
	return &ban, nil
}

func (s *SQLStore) ListBans(ctx context.Context, includeInactive bool, now time.Time) ([]Ban, error) {
	var rows []banRow
	query := s.db.NewSelect().Model(&rows).Order("created_at DESC")
	if !includeInactive {
		query = query.Where("lifted = ?", false).Where("expires_at IS NULL OR expires_at > ?", now)
	}
	if err := query.Scan(ctx); err != nil {
		return nil, err
	}
	return toBans(rows), nil
}

func (s *SQLStore) LiftBan(ctx context.Context, reference string) error {
	_, err := s.db.NewUpdate().Model((*banRow)(nil)).Set("lifted = ?", true).Where("reference = ?", reference).Exec(ctx)
	return err
}

func (s *SQLStore) Prune(ctx context.Context, idleBefore, ipBefore time.Time) (int, error) {
	var stale []string
	err := s.db.NewSelect().
		Model((*machineRow)(nil)).
		Column("id").
		Where("last_seen < ?", idleBefore).
		Where("id NOT IN (SELECT target FROM hwid_ban WHERE lifted = ? AND (expires_at IS NULL OR expires_at > ?))", false, idleBefore).
		Scan(ctx, &stale)
	if err != nil {
		return 0, err
	}
	if len(stale) > 0 {
		for _, model := range []any{(*signalRow)(nil), (*accountRow)(nil)} {
			if _, err := s.db.NewDelete().Model(model).Where("machine_id IN (?)", bun.In(stale)).Exec(ctx); err != nil {
				return 0, err
			}
		}
		if _, err := s.db.NewDelete().Model((*machineRow)(nil)).Where("id IN (?)", bun.In(stale)).Exec(ctx); err != nil {
			return 0, err
		}
	}
	_, err = s.db.NewUpdate().Model((*machineRow)(nil)).Set("last_ip = ?", "").Where("last_seen < ?", ipBefore).Exec(ctx)
	return len(stale), err
}

func (s *SQLStore) Close() error { return s.db.Close() }

func (r machineRow) toMachine() Machine {
	machine := Machine{
		ID:        r.ID,
		ClusterID: r.ClusterID,
		Platform:  r.Platform,
		Trusted:   r.Trusted,
		FirstSeen: r.FirstSeen,
		LastSeen:  r.LastSeen,
		LastIP:    r.LastIP,
	}
	if r.Flags != "" {
		machine.Flags = strings.Split(r.Flags, ",")
	}
	return machine
}

func (r banRow) toBan() Ban {
	return Ban{
		Reference: r.Reference,
		Scope:     apiv1.BanScope(r.Scope),
		Target:    r.Target,
		Reason:    r.Reason,
		By:        r.By,
		CreatedAt: r.CreatedAt,
		ExpiresAt: r.ExpiresAt,
		Lifted:    r.Lifted,
	}
}

func toMachines(rows []machineRow) []Machine {
	out := make([]Machine, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toMachine())
	}
	return out
}

func toAccounts(rows []accountRow) []Account {
	out := make([]Account, 0, len(rows))
	for _, row := range rows {
		out = append(out, Account{Subject: row.Subject, Username: row.Username, LastSeen: row.LastSeen})
	}
	return out
}

func toBans(rows []banRow) []Ban {
	out := make([]Ban, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toBan())
	}
	return out
}
