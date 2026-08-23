package access

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
)

type Subject struct {
	Subject  string
	Username string
	UUID     string
}

func (s Subject) Anonymous() bool {
	return s.Subject == "" && s.Username == "" && s.UUID == ""
}

type Source interface {
	Allows(ctx context.Context, build string, subject Subject) (bool, error)
}

type Reloader interface {
	Reload()
}

type SourceFactory func(config json.RawMessage) (Source, error)

var sourceFactories = map[string]SourceFactory{}

func RegisterSource(name string, factory SourceFactory) {
	sourceFactories[name] = factory
}

func SourceNames() []string {
	names := make([]string, 0, len(sourceFactories))
	for name := range sourceFactories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func buildSource(kind string, config json.RawMessage) (Source, error) {
	factory, ok := sourceFactories[kind]
	if !ok {
		return nil, fmt.Errorf("unknown access source type %q (have %s)", kind, strings.Join(SourceNames(), ", "))
	}
	return factory(config)
}

type Decision struct {
	Allowed bool
	Hidden  bool
	Reason  string
}

var allow = Decision{Allowed: true}

const defaultDeniedReason = "Доступ к этой сборке выдаётся вручную"

type SourceConfig struct {
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

type RuleConfig struct {
	Builds     []string `json:"builds"`
	Source     string   `json:"source"`
	Visibility string   `json:"visibility"`
	Message    string   `json:"message"`
}

type Config struct {
	Sources       map[string]SourceConfig `json:"sources"`
	Rules         []RuleConfig            `json:"rules"`
	PublicObjects bool                    `json:"publicObjects"`
}

type rule struct {
	patterns []string
	name     string
	source   Source
	hidden   bool
	message  string
}

type RuleInfo struct {
	Builds  []string
	Source  string
	Hidden  bool
	Message string
}

func (r *rule) matches(build string) bool {
	for _, pattern := range r.patterns {
		if ok, err := path.Match(pattern, build); err == nil && ok {
			return true
		}
	}
	return false
}

type Controller struct {
	rules         []rule
	sources       map[string]Source
	publicObjects bool
}

func New(cfg *Config) (*Controller, error) {
	if cfg == nil {
		return nil, nil
	}
	sources := make(map[string]Source, len(cfg.Sources))
	for name, sc := range cfg.Sources {
		source, err := buildSource(sc.Type, sc.Config)
		if err != nil {
			return nil, fmt.Errorf("access source %q: %w", name, err)
		}
		sources[name] = source
	}
	controller := &Controller{sources: sources, publicObjects: cfg.PublicObjects}
	for i, rc := range cfg.Rules {
		source, ok := sources[rc.Source]
		if !ok {
			return nil, fmt.Errorf("access rule %d references unknown source %q", i, rc.Source)
		}
		if len(rc.Builds) == 0 {
			return nil, fmt.Errorf("access rule %d matches no builds", i)
		}
		visibility := strings.ToLower(rc.Visibility)
		if visibility != "" && visibility != "listed" && visibility != "hidden" {
			return nil, fmt.Errorf("access rule %d has unknown visibility %q (want listed or hidden)", i, rc.Visibility)
		}
		message := rc.Message
		if message == "" {
			message = defaultDeniedReason
		}
		controller.rules = append(controller.rules, rule{
			patterns: rc.Builds,
			name:     rc.Source,
			source:   source,
			hidden:   visibility == "hidden",
			message:  message,
		})
	}
	return controller, nil
}

func (c *Controller) Guarded() bool {
	return c != nil && len(c.rules) > 0 && !c.publicObjects
}

func (c *Controller) Decide(ctx context.Context, build string, subject Subject) Decision {
	if c == nil {
		return allow
	}
	for i := range c.rules {
		r := &c.rules[i]
		if !r.matches(build) {
			continue
		}
		if subject.Anonymous() {
			return Decision{Hidden: r.hidden, Reason: r.message}
		}
		allowed, err := r.source.Allows(ctx, build, subject)
		if err != nil {
			return Decision{Hidden: r.hidden, Reason: "Не удалось проверить доступ, попробуйте позже"}
		}
		if !allowed {
			return Decision{Hidden: r.hidden, Reason: r.message}
		}
		return allow
	}
	return allow
}

func (c *Controller) Describe() []RuleInfo {
	if c == nil {
		return nil
	}
	out := make([]RuleInfo, 0, len(c.rules))
	for i := range c.rules {
		r := &c.rules[i]
		out = append(out, RuleInfo{Builds: r.patterns, Source: r.name, Hidden: r.hidden, Message: r.message})
	}
	return out
}

func (c *Controller) Reload() {
	if c == nil {
		return
	}
	for _, source := range c.sources {
		if reloader, ok := source.(Reloader); ok {
			reloader.Reload()
		}
	}
}

type Memo struct {
	controller *Controller
	subject    Subject
	mu         sync.Mutex
	seen       map[string]Decision
}

func (c *Controller) Memo(subject Subject) *Memo {
	return &Memo{controller: c, subject: subject, seen: map[string]Decision{}}
}

func (m *Memo) Decide(ctx context.Context, build string) Decision {
	m.mu.Lock()
	cached, ok := m.seen[build]
	m.mu.Unlock()
	if ok {
		return cached
	}
	decision := m.controller.Decide(ctx, build, m.subject)
	m.mu.Lock()
	m.seen[build] = decision
	m.mu.Unlock()
	return decision
}
