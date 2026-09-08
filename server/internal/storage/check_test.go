package storage

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/laminara/laminara/server/internal/diag"
)

type recordingBackend struct {
	objects map[string][]byte
	events  []string
}

func (b *recordingBackend) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if b.objects == nil {
		b.objects = map[string][]byte{}
	}
	b.objects[key] = data
	b.events = append(b.events, "put")
	return nil
}

func (b *recordingBackend) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.objects[key])), nil
}

func (b *recordingBackend) Stat(_ context.Context, key string) (int64, bool, error) {
	data, ok := b.objects[key]
	return int64(len(data)), ok, nil
}

func (b *recordingBackend) Delete(_ context.Context, key string) error {
	delete(b.objects, key)
	b.events = append(b.events, "delete")
	return nil
}

func (b *recordingBackend) Locate(_ context.Context, key string, _ time.Duration) (Location, error) {
	b.events = append(b.events, "locate")
	if _, alive := b.objects[key]; !alive {
		return Location{}, io.EOF
	}
	return Location{Kind: LocationURL, URL: "https://cdn.example/" + key}, nil
}

func TestProbeLinkIsCheckedBeforeTheObjectIsRemoved(t *testing.T) {
	backend := &recordingBackend{}
	probe := diag.New("storage")

	var sawKey string
	ok := roundtrip(context.Background(), backend, probe, func(key string) {
		sawKey = key
		if _, alive := backend.objects[key]; !alive {
			t.Fatal("пробный объект удалён до проверки ссылки — игроку она отдаст 404")
		}
	})

	if !ok {
		t.Fatalf("проба не прошла: %+v", probe.Results())
	}
	if sawKey == "" {
		t.Fatal("проверка при живом объекте не вызвана")
	}
	if len(backend.events) == 0 || backend.events[len(backend.events)-1] != "delete" {
		t.Fatalf("удаление должно быть последним действием, получили %v", backend.events)
	}
	if len(backend.objects) != 0 {
		t.Fatalf("проба осталась в хранилище: %v", backend.objects)
	}
}

func TestPrivateHostsAreRefused(t *testing.T) {
	for _, host := range []string{"minio", "127.0.0.1", "10.0.0.5", "localhost", "storage.local"} {
		if _, private := unreachableFromOutside(host); !private {
			t.Errorf("%s должен считаться недоступным игроку", host)
		}
	}
	for _, host := range []string{"s3.example.com", "cdn.example.org", "203.0.113.10"} {
		if reason, private := unreachableFromOutside(host); private {
			t.Errorf("%s ошибочно признан внутренним: %s", host, reason)
		}
	}
}
