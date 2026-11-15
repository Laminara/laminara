package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/storage"
)

func TestCASRepublishingRepairsCorruptObjects(t *testing.T) {
	for _, backend := range []struct {
		name  string
		build func(*testing.T) storage.Backend
	}{
		{"fs", newBackend},
		{"streamed-fs", func(t *testing.T) storage.Backend { return &plainBackend{Backend: newBackend(t)} }},
		{"s3", newS3Backend},
	} {
		t.Run(backend.name, func(t *testing.T) { testCASRepair(t, backend.build) })
	}
}

func testCASRepair(t *testing.T, build func(*testing.T) storage.Backend) {
	for _, algo := range []corev1.HashAlgo{corev1.HashAlgo_HASH_ALGO_BLAKE3, corev1.HashAlgo_HASH_ALGO_SHA256} {
		for _, file := range []bool{false, true} {
			for _, corrupt := range [][]byte{[]byte("x"), []byte("broken content"), []byte("longer corrupted object")} {
				t.Run(fmt.Sprintf("%s/file=%v/corrupt=%d", algo, file, len(corrupt)), func(t *testing.T) {
					ctx := context.Background()
					backend := build(t)
					cas := storage.NewCAS(backend, algo)
					payload := []byte("intact content")
					source := writeSource(t, payload)
					publish := func() (*corev1.ObjectRef, error) {
						if file {
							return cas.PutFile(ctx, source)
						}
						return cas.Put(ctx, bytes.NewReader(payload))
					}
					first, err := publish()
					if err != nil {
						t.Fatal(err)
					}
					key := storage.ObjectKey(first.Hash.Algo, first.Hash.Value)
					if err := backend.Put(ctx, key, bytes.NewReader(corrupt), int64(len(corrupt))); err != nil {
						t.Fatal(err)
					}
					again, err := publish()
					if err != nil {
						t.Fatal(err)
					}
					if again.Size != first.Size || !bytes.Equal(again.Hash.Value, first.Hash.Value) {
						t.Fatal("repair changed the object reference")
					}
					reader, err := cas.Get(ctx, again)
					if err != nil {
						t.Fatal(err)
					}
					got, err := io.ReadAll(reader)
					reader.Close()
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, payload) {
						t.Fatalf("republishing kept corrupt bytes: %q", got)
					}
				})
			}
		}
	}
}

type repairBackend struct {
	storage.Backend
	statError, getError, readError, closeError, putError error
	onStat                                               func() error
	onPut                                                func(io.Reader) error
	onEOF, onClose                                       func()
	puts, deletes                                        int
}

func (b *repairBackend) Stat(ctx context.Context, key string) (int64, bool, error) {
	if b.statError != nil {
		return 0, false, b.statError
	}
	size, exists, err := b.Backend.Stat(ctx, key)
	if err == nil && b.onStat != nil {
		err = b.onStat()
	}
	return size, exists, err
}

func (b *repairBackend) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if b.getError != nil {
		return nil, b.getError
	}
	reader, err := b.Backend.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return &repairReader{ReadCloser: reader, readError: b.readError, closeError: b.closeError, onEOF: b.onEOF, onClose: b.onClose}, nil
}

func (b *repairBackend) Put(ctx context.Context, key string, reader io.Reader, size int64) error {
	b.puts++
	if b.onPut != nil {
		if err := b.onPut(reader); err != nil {
			return err
		}
	}
	if b.putError != nil {
		return b.putError
	}
	return b.Backend.Put(ctx, key, reader, size)
}

func (b *repairBackend) Delete(ctx context.Context, key string) error {
	b.deletes++
	return b.Backend.Delete(ctx, key)
}

type repairReader struct {
	io.ReadCloser
	readError, closeError error
	onEOF, onClose        func()
}

func (r *repairReader) Read(buffer []byte) (int, error) {
	if r.readError != nil {
		return 0, r.readError
	}
	n, err := r.ReadCloser.Read(buffer)
	if errors.Is(err, io.EOF) && r.onEOF != nil {
		r.onEOF()
	}
	return n, err
}

func (r *repairReader) Close() error {
	err := r.ReadCloser.Close()
	if r.onClose != nil {
		r.onClose()
	}
	return errors.Join(err, r.closeError)
}

func TestCASRepairDoesNotOverwriteOrDeleteOnStorageErrors(t *testing.T) {
	for _, file := range []bool{false, true} {
		for _, stage := range []string{"stat", "get", "read", "close", "write", "healthy"} {
			t.Run(fmt.Sprintf("file=%v/%s", file, stage), func(t *testing.T) {
				ctx := context.Background()
				inner := newBackend(t)
				payload, corrupt := []byte("intact content"), []byte("broken content")
				ref := refOf(t, payload)
				key := storage.ObjectKey(ref.Hash.Algo, ref.Hash.Value)
				stored := corrupt
				if stage == "healthy" {
					stored = payload
				}
				if err := inner.Put(ctx, key, bytes.NewReader(stored), int64(len(stored))); err != nil {
					t.Fatal(err)
				}
				failure := errors.New("storage failure")
				backend := &repairBackend{Backend: inner}
				switch stage {
				case "stat":
					backend.statError = failure
				case "get":
					backend.getError = failure
				case "read":
					backend.readError = failure
				case "close":
					backend.closeError = failure
				case "write", "healthy":
					backend.putError = failure
				}
				cas := storage.NewCAS(backend, ref.Hash.Algo)
				var err error
				if file {
					_, err = cas.PutFile(ctx, writeSource(t, payload))
				} else {
					_, err = cas.Put(ctx, bytes.NewReader(payload))
				}
				if stage == "healthy" {
					if err != nil {
						t.Fatalf("valid object was not reused: %v", err)
					}
				} else if !errors.Is(err, failure) {
					t.Fatalf("got %v, want storage error", err)
				}
				wantWrites := 0
				if stage == "write" {
					wantWrites = 1
				}
				if backend.puts != wantWrites || backend.deletes != 0 {
					t.Fatalf("unexpected mutations: puts=%d deletes=%d", backend.puts, backend.deletes)
				}
				reader, err := inner.Get(ctx, key)
				if err != nil {
					t.Fatal(err)
				}
				got, err := io.ReadAll(reader)
				reader.Close()
				if err != nil || !bytes.Equal(got, stored) {
					t.Fatalf("existing object was changed: %q, %v", got, err)
				}
			})
		}
	}
}

func TestCASRepairPreservesTheOldObjectWhenTheSourceChanges(t *testing.T) {
	for _, fileSupport := range []bool{false, true} {
		t.Run(fmt.Sprintf("fileSupport=%v", fileSupport), func(t *testing.T) {
			ctx := context.Background()
			payload, corrupt := []byte("intact content"), []byte("broken content")
			source := writeSource(t, payload)
			inner := newBackend(t)
			ref := refOf(t, payload)
			key := storage.ObjectKey(ref.Hash.Algo, ref.Hash.Value)
			if err := inner.Put(ctx, key, bytes.NewReader(corrupt), int64(len(corrupt))); err != nil {
				t.Fatal(err)
			}
			var backend storage.Backend
			if fileSupport {
				backend = &racingBackend{Backend: inner, source: source, replace: []byte("changed source")}
			} else {
				backend = &repairBackend{Backend: inner, onStat: func() error { return os.WriteFile(source, []byte("changed source"), 0o644) }}
			}
			_, err := storage.NewCAS(backend, ref.Hash.Algo).PutFile(ctx, source)
			if err == nil || !strings.Contains(err.Error(), "изменился во время публикации") {
				t.Fatalf("expected changed-source error, got %v", err)
			}
			reader, err := inner.Get(ctx, key)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || !bytes.Equal(got, corrupt) {
				t.Fatalf("failed repair changed old object: %q, %v", got, err)
			}
		})
	}
}

func TestCASEmptyObjectRepair(t *testing.T) {
	testCASEmptyRepair(t, newBackend(t))
}

func testCASEmptyRepair(t *testing.T, backend storage.Backend) {
	for _, file := range []bool{false, true} {
		t.Run(fmt.Sprintf("empty/file=%v", file), func(t *testing.T) {
			ctx := context.Background()
			cas := storage.NewCAS(backend, corev1.HashAlgo_HASH_ALGO_BLAKE3)
			source := writeSource(t, nil)
			publish := func() (*corev1.ObjectRef, error) {
				if file {
					return cas.PutFile(ctx, source)
				}
				return cas.Put(ctx, bytes.NewReader(nil))
			}
			first, err := publish()
			if err != nil {
				t.Fatal(err)
			}
			key := storage.ObjectKey(first.Hash.Algo, first.Hash.Value)
			if err := backend.Put(ctx, key, strings.NewReader("broken"), 6); err != nil {
				t.Fatal(err)
			}
			ref, err := publish()
			if err != nil {
				t.Fatal(err)
			}
			reader, err := cas.Get(ctx, ref)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || len(got) != 0 || ref.Size != 0 {
				t.Fatalf("empty object was not restored: %q, %v", got, err)
			}
		})
	}
}

func TestCASTemporaryFilesAreRemovedOnSuccessFailureAndCancellation(t *testing.T) {
	for _, file := range []bool{false, true} {
		for _, outcome := range []string{"success", "write-error", "read-error", "cancelled", "cancelled-upload"} {
			t.Run(fmt.Sprintf("file=%v/%s", file, outcome), func(t *testing.T) {
				payload, corrupt := []byte("intact content"), []byte("broken content")
				source := writeSource(t, payload)
				root := t.TempDir()
				config, err := json.Marshal(map[string]string{"root": root})
				if err != nil {
					t.Fatal(err)
				}
				inner, err := storage.BuildBackend("fs", config)
				if err != nil {
					t.Fatal(err)
				}
				ref := refOf(t, payload)
				key := storage.ObjectKey(ref.Hash.Algo, ref.Hash.Value)
				if err := inner.Put(context.Background(), key, bytes.NewReader(corrupt), int64(len(corrupt))); err != nil {
					t.Fatal(err)
				}
				scratch := t.TempDir()
				for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
					t.Setenv(name, scratch)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				failure := errors.New("injected storage failure")
				backend := &repairBackend{Backend: inner}
				if outcome == "write-error" {
					backend.putError = failure
				}
				if outcome == "read-error" {
					backend.readError = failure
				}
				if outcome == "cancelled" {
					cancel()
				}
				backend.onPut = func(reader io.Reader) error {
					if runtime.GOOS != "windows" {
						entries, err := os.ReadDir(scratch)
						if err != nil {
							return err
						}
						if len(entries) != 0 {
							return errors.New("named staging files remain during upload")
						}
					}
					at, ok := reader.(io.ReaderAt)
					if !ok {
						return errors.New("upload lost ReaderAt")
					}
					seeker, ok := reader.(io.ReadSeeker)
					if !ok {
						return errors.New("upload lost ReadSeeker")
					}
					if outcome == "cancelled-upload" {
						cancel()
						for _, read := range []func() error{
							func() error { _, err := reader.Read(make([]byte, 1)); return err },
							func() error { _, err := at.ReadAt(make([]byte, 1), 0); return err },
							func() error { _, err := seeker.Seek(0, io.SeekStart); return err },
						} {
							if !errors.Is(read(), context.Canceled) {
								return errors.New("file access ignored cancellation")
							}
						}
					}
					return nil
				}
				cas := storage.NewCAS(backend, ref.Hash.Algo)
				if file {
					_, err = cas.PutFile(ctx, source)
				} else {
					_, err = cas.Put(ctx, bytes.NewReader(payload))
				}
				switch outcome {
				case "success":
					if err != nil {
						t.Fatal(err)
					}
				case "cancelled", "cancelled-upload":
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("got %v, want cancellation", err)
					}
				default:
					if !errors.Is(err, failure) {
						t.Fatalf("got %v, want storage error", err)
					}
				}
				entries, err := os.ReadDir(scratch)
				if err != nil || len(entries) != 0 {
					t.Fatalf("temporary files remain: %v, %v", entries, err)
				}
				err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if strings.HasPrefix(entry.Name(), ".tmp-") {
						return fmt.Errorf("temporary file remains: %s", path)
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				reader, err := inner.Get(context.Background(), key)
				if err != nil {
					t.Fatal(err)
				}
				got, err := io.ReadAll(reader)
				reader.Close()
				expected := corrupt
				if outcome == "success" {
					expected = payload
				}
				if err != nil || !bytes.Equal(got, expected) {
					t.Fatalf("unexpected stored bytes: %q, %v", got, err)
				}
			})
		}
	}
}

func TestCASConcurrentRepair(t *testing.T) {
	for _, backend := range []struct {
		name  string
		build func(*testing.T) storage.Backend
	}{{"fs", newBackend}, {"s3", newS3Backend}} {
		t.Run(backend.name, func(t *testing.T) { testCASConcurrentRepair(t, backend.build(t)) })
	}
}

func testCASConcurrentRepair(t *testing.T, backend storage.Backend) {
	ctx := context.Background()
	payload := bytes.Repeat([]byte("intact content"), 8192)
	source := writeSource(t, payload)
	cas := storage.NewCAS(backend, corev1.HashAlgo_HASH_ALGO_BLAKE3)
	ref, err := cas.PutFile(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	key := storage.ObjectKey(ref.Hash.Algo, ref.Hash.Value)
	corrupt := bytes.Repeat([]byte("broken content"), 8192)
	if err := backend.Put(ctx, key, bytes.NewReader(corrupt), int64(len(corrupt))); err != nil {
		t.Fatal(err)
	}
	const writers = 8
	start := make(chan struct{})
	results := make(chan error, writers)
	for worker := range writers {
		go func() {
			<-start
			var actual *corev1.ObjectRef
			var err error
			if worker%2 == 0 {
				actual, err = cas.PutFile(ctx, source)
			} else {
				actual, err = cas.Put(ctx, bytes.NewReader(payload))
			}
			if err == nil && (actual.Size != ref.Size || !bytes.Equal(actual.Hash.Value, ref.Hash.Value)) {
				err = errors.New("object reference changed")
			}
			results <- err
		}()
	}
	close(start)
	for range writers {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	reader, err := cas.Get(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("concurrent repair lost the object: %v", err)
	}
}

func TestCASDoesNotReportSuccessWhenVerificationFinishesAfterCancellation(t *testing.T) {
	for _, file := range []bool{false, true} {
		for _, when := range []string{"last-read", "close"} {
			t.Run(fmt.Sprintf("file=%v/%s", file, when), func(t *testing.T) {
				payload := []byte("intact content")
				inner := newBackend(t)
				ref, err := storage.NewCAS(inner, corev1.HashAlgo_HASH_ALGO_BLAKE3).Put(context.Background(), bytes.NewReader(payload))
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				backend := &repairBackend{Backend: inner}
				if when == "last-read" {
					backend.onEOF = cancel
				} else {
					backend.onClose = cancel
				}
				cas := storage.NewCAS(backend, ref.Hash.Algo)
				if file {
					_, err = cas.PutFile(ctx, writeSource(t, payload))
				} else {
					_, err = cas.Put(ctx, bytes.NewReader(payload))
				}
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("got %v, want cancellation", err)
				}
				if backend.puts != 0 || backend.deletes != 0 {
					t.Fatal("cancelled verification changed storage")
				}
			})
		}
	}
}

func TestCASStagingDoesNotLeaveFilesAfterProcessExit(t *testing.T) {
	if os.Getenv("LAMINARA_TEST_CAS_CRASH") == "1" {
		raw, err := json.Marshal(map[string]string{"root": os.Getenv("LAMINARA_TEST_CAS_ROOT")})
		if err != nil {
			t.Fatal(err)
		}
		inner, err := storage.BuildBackend("fs", raw)
		if err != nil {
			t.Fatal(err)
		}
		payload := []byte("snapshot before abrupt process exit")
		backend := &repairBackend{Backend: inner, onPut: func(reader io.Reader) error {
			got, err := io.ReadAll(reader)
			if err != nil || !bytes.Equal(got, payload) {
				os.Exit(38)
			}
			os.Exit(37)
			return nil
		}}
		_, err = storage.NewCAS(backend, corev1.HashAlgo_HASH_ALGO_BLAKE3).Put(context.Background(), bytes.NewReader(payload))
		t.Fatalf("helper did not exit: %v", err)
	}
	scratch := t.TempDir()
	command := exec.Command(os.Args[0], "-test.run=^TestCASStagingDoesNotLeaveFilesAfterProcessExit$")
	command.Env = append(os.Environ(), "LAMINARA_TEST_CAS_CRASH=1", "LAMINARA_TEST_CAS_ROOT="+t.TempDir(), "TMPDIR="+scratch, "TMP="+scratch, "TEMP="+scratch)
	output, err := command.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 37 {
		t.Fatalf("unexpected helper result: %v, %s", err, output)
	}
	entries, err := os.ReadDir(scratch)
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging files remain after process exit: %v, %v", entries, err)
	}
}
