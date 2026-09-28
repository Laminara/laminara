package crash

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

const (
	defaultMaxPerHour       = 20
	defaultAnonymousPerHour = 3
)

type Service struct {
	sinks          []Sink
	limit          int
	anonymous      bool
	anonymousLimit int

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
		limit:          cfg.MaxPerHour,
		anonymous:      cfg.acceptsAnonymous(),
		anonymousLimit: cfg.AnonymousPerHour,
		recent:         map[string][]time.Time{},
		now:            time.Now,
	}
	if service.limit <= 0 {
		service.limit = defaultMaxPerHour
	}
	if service.anonymousLimit <= 0 {
		service.anonymousLimit = defaultAnonymousPerHour
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
	if !s.allow(report.Player, s.limit) {
		return fmt.Errorf("отчётов от %s за последний час уже достаточно", report.Player)
	}
	return s.deliver(ctx, report, log)
}

func (s *Service) AcceptAnonymous(ctx context.Context, report Report, address string, log *slog.Logger) error {
	if s == nil {
		return fmt.Errorf("приём отчётов выключен")
	}
	if !s.anonymous {
		return fmt.Errorf("сервер принимает журнал только после входа")
	}
	if !s.allow("адрес "+address, s.anonymousLimit) {
		return fmt.Errorf("журналов с этого адреса за последний час уже достаточно")
	}
	report.Player = ""
	report.UUID = ""
	report.Address = address
	return s.deliver(ctx, report, log)
}

func (s *Service) deliver(ctx context.Context, report Report, log *slog.Logger) error {
	var failed int
	for _, sink := range s.sinks {
		if err := sink.Send(ctx, report); err != nil {
			failed++
			log.Error("отчёт не доставлен", "source", "crash", "вид", report.Kind.String(), "куда", sink.Name(), "ошибка", err)
		}
	}
	if failed == len(s.sinks) {
		return fmt.Errorf("отчёт не удалось доставить никуда")
	}
	log.Info("отчёт принят",
		"source", "crash",
		"вид", report.Kind.String(),
		"игрок", report.who(),
		"адрес", report.Address,
		"сборка", report.Build,
		"код", report.ExitCode,
	)
	return nil
}

func (s *Service) forgetStale(deadline time.Time) {
	for name, moments := range s.recent {
		if len(moments) == 0 || moments[len(moments)-1].Before(deadline) {
			delete(s.recent, name)
		}
	}
}

func (s *Service) allow(sender string, limit int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	deadline := s.now().Add(-time.Hour)
	kept := s.recent[sender][:0]
	for _, moment := range s.recent[sender] {
		if moment.After(deadline) {
			kept = append(kept, moment)
		}
	}
	if len(kept) == 0 {
		delete(s.recent, sender)
	} else {
		s.recent[sender] = kept
	}
	s.forgetStale(deadline)

	if len(kept) >= limit {
		return false
	}
	s.recent[sender] = append(s.recent[sender], s.now())
	return true
}
