package storage_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/minio/minio-go/v7"
)

func TestS3MultipartCleanupKeepsParallelUploadsAndReportsFailures(t *testing.T) {
	payload := bytes.Repeat([]byte{0x5a}, 18<<20)
	for _, failure := range []string{"part", "complete", "abort", "empty-id"} {
		t.Run(failure, func(t *testing.T) {
			var armed atomic.Bool
			var mu sync.Mutex
			var aborted []string
			backend, client := newS3Fixture(t, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					query := r.URL.Query()
					if armed.Load() {
						reject := false
						code := "InvalidRequest"
						if r.Method == http.MethodDelete && query.Get("uploadId") != "" {
							mu.Lock()
							aborted = append(aborted, query.Get("uploadId"))
							mu.Unlock()
							if failure == "abort" {
								reject, code = true, "AccessDenied"
							}
						}
						if r.Method == http.MethodPut && query.Get("partNumber") == "2" && (failure == "part" || failure == "abort") {
							reject = true
						}
						if r.Method == http.MethodPost && query.Get("uploadId") != "" && failure == "complete" {
							reject = true
						}
						if r.Method == http.MethodPost && query.Has("uploads") && failure == "empty-id" {
							w.Header().Set("Content-Type", "application/xml")
							fmt.Fprint(w, "<InitiateMultipartUploadResult><UploadId></UploadId></InitiateMultipartUploadResult>")
							return
						}
						if reject {
							w.Header().Set("Content-Type", "application/xml")
							status := http.StatusBadRequest
							if code == "AccessDenied" {
								status = http.StatusForbidden
							}
							w.WriteHeader(status)
							fmt.Fprintf(w, "<Error><Code>%s</Code><Message>injected %s failure</Message></Error>", code, failure)
							return
						}
					}
					next.ServeHTTP(w, r)
				})
			})
			ctx := context.Background()
			key := "objects/repair"
			if err := backend.Put(ctx, key, strings.NewReader("old"), 3); err != nil {
				t.Fatal(err)
			}
			core := minio.Core{Client: client}
			foreign, err := core.NewMultipartUpload(ctx, "laminara", key, minio.PutObjectOptions{})
			if err != nil {
				t.Fatal(err)
			}
			armed.Store(true)
			scratch := t.TempDir()
			for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
				t.Setenv(name, scratch)
			}
			err = backend.Put(ctx, key, bytes.NewBuffer(payload), int64(len(payload)))
			if err == nil {
				t.Fatal("injected failure was ignored")
			}
			if failure == "abort" {
				if !strings.Contains(err.Error(), "не удалось убрать незавершённую загрузку") {
					t.Fatalf("cleanup failure was hidden: %v", err)
				}
			}
			if failure != "empty-id" {
				var response minio.ErrorResponse
				if !errors.As(err, &response) || response.Code != "InvalidRequest" {
					t.Fatalf("original failure was lost: %v", err)
				}
			}
			mu.Lock()
			ids := append([]string(nil), aborted...)
			mu.Unlock()
			wantAborts := 1
			if failure == "empty-id" {
				wantAborts = 0
			}
			if len(ids) != wantAborts {
				t.Fatalf("abort calls = %v, want %d", ids, wantAborts)
			}
			for _, id := range ids {
				if id == foreign {
					t.Fatal("parallel upload was aborted")
				}
			}
			pending := 0
			for upload := range client.ListIncompleteUploads(ctx, "laminara", key, true) {
				if upload.Err != nil {
					t.Fatal(upload.Err)
				}
				pending++
				if failure != "abort" && upload.UploadID != foreign {
					t.Fatal("own failed upload was not removed")
				}
			}
			wantPending := 1
			if failure == "abort" {
				wantPending = 2
			}
			if pending != wantPending {
				t.Fatalf("pending uploads = %d, want %d", pending, wantPending)
			}
			reader, err := backend.Get(ctx, key)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || string(got) != "old" {
				t.Fatalf("old object changed: %q, %v", got, err)
			}
			entries, err := os.ReadDir(scratch)
			if err != nil || len(entries) != 0 {
				t.Fatalf("local staging files remain: %v, %v", entries, err)
			}
		})
	}
}

func TestS3MultipartSupportsCurrentPositionAndUnknownStreamSize(t *testing.T) {
	payload := bytes.Repeat([]byte{0x51}, 17<<20)
	for _, unknown := range []bool{false, true} {
		t.Run(fmt.Sprintf("unknown=%v", unknown), func(t *testing.T) {
			backend := newS3Backend(t)
			var input io.Reader
			size := int64(len(payload))
			if unknown {
				input, size = bytes.NewBuffer(payload), -1
			} else {
				positioned := bytes.NewReader(append([]byte("prefix"), payload...))
				if _, err := positioned.Seek(6, io.SeekStart); err != nil {
					t.Fatal(err)
				}
				input = positioned
			}
			if err := backend.Put(context.Background(), "positioned", input, size); err != nil {
				t.Fatal(err)
			}
			reader, err := backend.Get(context.Background(), "positioned")
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatalf("wrong multipart content: got=%d want=%d prefix=%q err=%v", len(got), len(payload), got[:min(100, len(got))], err)
			}
		})
	}
}

func TestS3StagingConsumesOnlyTheDeclaredStreamSize(t *testing.T) {
	backend := newS3Backend(t)
	payload := bytes.Repeat([]byte{0x41}, 17<<20)
	input := bytes.NewBuffer(append(payload, []byte("trailer")...))
	if err := backend.Put(context.Background(), "bounded", input, int64(len(payload))); err != nil {
		t.Fatal(err)
	}
	if input.String() != "trailer" {
		t.Fatal("upload consumed bytes after the declared object")
	}
	reader, err := backend.Get(context.Background(), "bounded")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("wrong bounded stream content: %v", err)
	}
}
