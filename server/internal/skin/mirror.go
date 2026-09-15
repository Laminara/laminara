package skin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/laminara/laminara/server/internal/diag"
	"github.com/laminara/laminara/server/internal/httpx"
)

const (
	mirrorTTL      = time.Minute
	mirrorFileCap  = 2 << 20
	mirrorTotalCap = 64 << 20
	mirrorURLCap   = 8192
)

type Mirror struct {
	inner  Provider
	client *http.Client
	base   string
	ttl    time.Duration
	now    func() time.Time

	mu    sync.Mutex
	names map[string]mirroredName
	kept  map[string][]byte
	order []string
	bytes int
}

type mirroredName struct {
	name    string
	fetched time.Time
}

func NewMirror(inner Provider, base string) *Mirror {
	return newMirror(inner, base, httpx.NewClient(8*time.Second), mirrorTTL, time.Now)
}

func newMirror(inner Provider, base string, client *http.Client, ttl time.Duration, now func() time.Time) *Mirror {
	return &Mirror{
		inner:  inner,
		client: client,
		base:   strings.TrimSuffix(base, "/"),
		ttl:    ttl,
		now:    now,
		names:  map[string]mirroredName{},
		kept:   map[string][]byte{},
	}
}

func (m *Mirror) Textures(ctx context.Context, username, uuid string) (Textures, error) {
	textures, err := m.inner.Textures(ctx, username, uuid)
	if err != nil {
		return textures, err
	}
	textures.SkinURL = m.mirrored(ctx, textures.SkinURL)
	textures.CapeURL = m.mirrored(ctx, textures.CapeURL)
	return textures, nil
}

func (m *Mirror) Check(ctx context.Context, probe *diag.Probe) {
	if checker, ok := m.inner.(diag.Checker); ok {
		checker.Check(ctx, probe)
	}
	probe.OK("зеркало скинов", "картинки раздаёт сам сервер: %s/<хеш картинки>", m.base)
}

func (m *Mirror) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, ok := m.image(path.Base(r.URL.Path))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(body)
}

func (m *Mirror) mirrored(ctx context.Context, raw string) string {
	if raw == "" || m.base == "" || strings.HasPrefix(raw, m.base+"/") {
		return raw
	}
	name, known := m.remembered(raw)
	if !known {
		fetched, answered := m.fetch(ctx, raw)
		name = fetched
		if !answered {
			name = m.lastKnown(raw)
		}
		m.remember(raw, name)
	}
	if name == "" {
		return raw
	}
	return m.base + "/" + name
}

func (m *Mirror) fetch(ctx context.Context, raw string) (string, bool) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", false
	}
	response, err := m.client.Do(request)
	if err != nil {
		return "", false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", true
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, mirrorFileCap+1))
	if err != nil {
		return "", false
	}
	if len(body) > mirrorFileCap || !looksLikePNG(body) {
		return "", true
	}
	sum := sha256.Sum256(body)
	name := hex.EncodeToString(sum[:])
	m.keep(name, body)
	return name, true
}

func looksLikePNG(body []byte) bool {
	return len(body) > 8 && string(body[1:4]) == "PNG"
}

func (m *Mirror) remembered(raw string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.names[raw]
	if !ok || m.now().Sub(entry.fetched) > m.ttl {
		return "", false
	}
	if entry.name != "" {
		if _, held := m.kept[entry.name]; !held {
			return "", false
		}
	}
	return entry.name, true
}

func (m *Mirror) lastKnown(raw string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.names[raw]
	if !ok || entry.name == "" {
		return ""
	}
	if _, held := m.kept[entry.name]; !held {
		return ""
	}
	return entry.name
}

func (m *Mirror) remember(raw, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.names) > mirrorURLCap {
		m.names = map[string]mirroredName{}
	}
	m.names[raw] = mirroredName{name: name, fetched: m.now()}
}

func (m *Mirror) keep(name string, body []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, held := m.kept[name]; held {
		return
	}
	m.kept[name] = body
	m.order = append(m.order, name)
	m.bytes += len(body)
	for m.bytes > mirrorTotalCap && len(m.order) > 1 {
		oldest := m.order[0]
		m.order = m.order[1:]
		m.bytes -= len(m.kept[oldest])
		delete(m.kept, oldest)
	}
}

func (m *Mirror) image(name string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	body, ok := m.kept[name]
	return body, ok
}
