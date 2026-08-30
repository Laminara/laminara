package news

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
)

const (
	defaultLimit    = 20
	defaultCacheTTL = 5 * time.Minute
	maxBodyRunes    = 2000
	maxTitleRunes   = 160
)

type Source interface {
	Items(ctx context.Context) ([]Item, error)
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

type SourceConfig struct {
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

type Config struct {
	Source SourceConfig `json:"source"`
	Limit  int          `json:"limit"`
}

type Item struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"publishedAt"`
	Tag         string    `json:"tag"`
	Link        string    `json:"link"`
	Banner      string    `json:"banner"`
}

type document struct {
	Items []Item `json:"items"`
}

func parse(data []byte) ([]Item, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, nil
	}
	var items []Item
	if err := json.Unmarshal([]byte(trimmed), &items); err == nil {
		return items, nil
	}
	var doc document
	if err := json.Unmarshal([]byte(trimmed), &doc); err != nil {
		return nil, fmt.Errorf("news is neither a list nor an object with \"items\": %w", err)
	}
	return doc.Items, nil
}

type Service struct {
	source  Source
	limit   int
	banners *bannerCache
}

func New(cfg *Config) (*Service, error) {
	if cfg == nil || cfg.Source.Type == "" {
		return nil, nil
	}
	factory, ok := sourceFactories[cfg.Source.Type]
	if !ok {
		return nil, fmt.Errorf("unknown news source %q (have %s)", cfg.Source.Type, strings.Join(SourceNames(), ", "))
	}
	source, err := factory(cfg.Source.Config)
	if err != nil {
		return nil, err
	}
	limit := cfg.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	return &Service{source: source, limit: limit, banners: newBannerCache()}, nil
}

func (s *Service) Enabled() bool { return s != nil }

func (s *Service) Latest(ctx context.Context) []*apiv1.NewsItem {
	if s == nil {
		return nil
	}
	items, err := s.source.Items(ctx)
	if err != nil {
		return nil
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].PublishedAt.After(items[j].PublishedAt) })
	if len(items) > s.limit {
		items = items[:s.limit]
	}

	out := make([]*apiv1.NewsItem, 0, len(items))
	for _, item := range items {
		title := clamp(item.Title, maxTitleRunes)
		if title == "" {
			continue
		}
		out = append(out, &apiv1.NewsItem{
			Id:                   identity(item),
			Title:                title,
			Body:                 clamp(item.Body, maxBodyRunes),
			PublishedAtUnixNanos: publishedNanos(item),
			Tag:                  clamp(item.Tag, 32),
			Link:                 safeLink(item.Link),
			BannerDataUri:        s.banners.inline(ctx, item.Banner),
		})
	}
	return out
}

func publishedNanos(item Item) int64 {
	if item.PublishedAt.IsZero() {
		return 0
	}
	return item.PublishedAt.UnixNano()
}

func identity(item Item) string {
	if item.ID != "" {
		return item.ID
	}
	return item.Title
}

func isWebLink(link string) bool {
	lower := strings.ToLower(strings.TrimSpace(link))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func safeLink(link string) string {
	if !isWebLink(link) {
		return ""
	}
	return strings.TrimSpace(link)
}

func clamp(value string, limit int) string {
	trimmed := strings.TrimSpace(value)
	runes := []rune(trimmed)
	if len(runes) <= limit {
		return trimmed
	}
	return strings.TrimSpace(string(runes[:limit])) + "…"
}
