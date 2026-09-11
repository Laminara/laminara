package prepare

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestATooManyRequestsAnswerIsRetriedNotFatal(t *testing.T) {
	content := "библиотека"
	sum := sha1.Sum([]byte(content))

	var asked atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if asked.Add(1) < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, content)
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "lib.jar")
	err := downloadFile(context.Background(), server.Client(), server.URL, dest, sha1Digest(hex.EncodeToString(sum[:])), false)
	if err != nil {
		t.Fatalf("429 от репозитория не должен ронять сборку: %v", err)
	}
	if asked.Load() != 3 {
		t.Fatalf("запросов было %d, ожидались две неудачи и успех", asked.Load())
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != content {
		t.Fatalf("файл = %q, %v", got, err)
	}
}

func TestAMissingFileIsNotRetried(t *testing.T) {
	var asked atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "lib.jar")
	err := downloadFile(context.Background(), server.Client(), server.URL, dest, digest{}, false)
	if err == nil {
		t.Fatal("404 обязан быть ошибкой")
	}
	if asked.Load() != 1 {
		t.Fatalf("404 повторяли %d раз — репозиторий просто не знает этот файл", asked.Load())
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("ошибка не называет код ответа: %v", err)
	}
}

func TestAFileThatArrivesBrokenIsFetchedAgain(t *testing.T) {
	content := "целый файл"
	sum := sha1.Sum([]byte(content))

	var asked atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if asked.Add(1) == 1 {
			fmt.Fprint(w, "обрывок")
			return
		}
		fmt.Fprint(w, content)
	}))
	defer server.Close()

	dest := filepath.Join(t.TempDir(), "lib.jar")
	if err := downloadFile(context.Background(), server.Client(), server.URL, dest, sha1Digest(hex.EncodeToString(sum[:])), false); err != nil {
		t.Fatalf("битая закачка должна повториться: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != content {
		t.Fatalf("файл = %q", got)
	}
}

func TestOneHostIsNotHammeredByEveryWorkerAtOnce(t *testing.T) {
	var inFlight, peak atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		now := inFlight.Add(1)
		for {
			seen := peak.Load()
			if now <= seen || peak.CompareAndSwap(seen, now) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
		fmt.Fprint(w, "файл")
	}))
	defer server.Close()

	dl := &downloader{http: server.Client(), root: t.TempDir(), workers: 12}
	jobs := make([]job, 0, 24)
	for i := range 24 {
		jobs = append(jobs, job{url: fmt.Sprintf("%s/%d", server.URL, i), path: fmt.Sprintf("libs/%d.jar", i)})
	}
	if err := dl.run(context.Background(), jobs, ""); err != nil {
		t.Fatal(err)
	}
	if peak.Load() > perHostLimit {
		t.Fatalf("на один хост ушло %d запросов разом, а репозитории за это отвечают 429", peak.Load())
	}
}

func TestAFullDiskIsNotMistakenForABrokenConnection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "содержимое")
	}))
	defer server.Close()

	occupied := filepath.Join(t.TempDir(), "занято")
	if err := os.MkdirAll(occupied, 0o500); err != nil {
		t.Fatal(err)
	}
	err := downloadFile(context.Background(), server.Client(), server.URL, filepath.Join(occupied, "lib.jar"), digest{}, false)
	if err == nil {
		t.Skip("файловая система разрешила запись в каталог только на чтение")
	}
	var again retryable
	if errors.As(err, &again) {
		t.Fatalf("ошибка записи повторяется как сетевая — пять раз впустую: %v", err)
	}
}

func TestAnExistingFileGetsItsExecutableBitEvenWhenNotDownloaded(t *testing.T) {
	content := "джава"
	sum := sha1.Sum([]byte(content))
	dest := filepath.Join(t.TempDir(), "java")
	if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("файл уже на месте — качать его не надо")
	}))
	defer server.Close()

	if err := downloadFile(context.Background(), server.Client(), server.URL, dest, sha1Digest(hex.EncodeToString(sum[:])), true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("права %v: java без бита исполнения не запустится, а install об этом промолчит", info.Mode().Perm())
	}
}
