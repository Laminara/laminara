package crash

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

const defaultMaxPerHour = 20

type Service struct {
	sinks []Sink
	limit int

	mu     sync.Mutex
	recent map[string][]time.Time
	now    func() time.Time
}

func New(cfg *Config) (*Service, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	names := make([]string, 0, len(cfg.Sinks))
	for name := range cfg.Sinks {
		names = append(names, name)
	}
	sort.Strings(names)

	service := &Service{
		limit:  cfg.MaxPerHour,
		recent: map[string][]time.Time{},
		now:    time.Now,
	}
	if service.limit <= 0 {
		service.limit = defaultMaxPerHour
	}

	for _, name := range names {
		entry := cfg.Sinks[name]
		factory, ok := factories[entry.Type]
		if !ok {
			return nil, fmt.Errorf("отчёты о падениях: «%s» — неизвестный способ доставки", entry.Type)
		}
		sink, err := factory(entry.Config)
		if err != nil {
			return nil, fmt.Errorf("отчёты о падениях, %s: %w", name, err)
		}
		service.sinks = append(service.sinks, sink)
	}

	if len(service.sinks) == 0 {
		return nil, fmt.Errorf("отчёты о падениях включены, но ни одного адреса доставки не задано")
	}
	return service, nil
}

func (s *Service) Accept(ctx context.Context, report Report, log *slog.Logger) error {
	if s == nil {
		return fmt.Errorf("приём отчётов о падениях выключен")
	}
	if !s.allow(report.Player) {
		return fmt.Errorf("отчётов от %s за последний час уже достаточно", report.Player)
	}

	var failed int
	for _, sink := range s.sinks {
		if err := sink.Send(ctx, report); err != nil {
			failed++
			log.Error("отчёт о падении не доставлен", "source", "crash", "куда", sink.Name(), "ошибка", err)
		}
	}
	if failed == len(s.sinks) {
		return fmt.Errorf("отчёт не удалось доставить никуда")
	}
	log.Info("отчёт о падении принят",
		"source", "crash",
		"игрок", report.Player,
		"сборка", report.Build,
		"код", report.ExitCode,
	)
	return nil
}

func (s *Service) allow(player string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	deadline := s.now().Add(-time.Hour)
	kept := s.recent[player][:0]
	for _, moment := range s.recent[player] {
		if moment.After(deadline) {
			kept = append(kept, moment)
		}
	}
	s.recent[player] = kept

	if len(kept) >= s.limit {
		return false
	}
	s.recent[player] = append(s.recent[player], s.now())
	return true
}
