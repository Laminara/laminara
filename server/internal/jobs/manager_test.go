package jobs

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/controlauth"
)

func TestJobSurvivesRequestCancellationAndDeduplicates(t *testing.T) {
	registry := command.NewRegistry()
	gate := make(chan struct{})
	var calls atomic.Int32
	registry.Register(command.Command{Name: "work", Aliases: []string{"alias"}, Run: func(ctx context.Context, args []string, out io.Writer) error {
		calls.Add(1)
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
		_, err := io.WriteString(out, "готово\n")
		return err
	}})
	dir := t.TempDir()
	manager, err := New(t.Context(), dir, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close(context.Background()) })
	ctx, cancel := context.WithCancel(t.Context())
	request := &adminv1.StartJobRequest{Arguments: []string{"alias", "hidden-secret"}, IdempotencyKey: "stable"}
	job, err := manager.Start(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	request.Arguments[0] = "work"
	repeated, err := manager.Start(t.Context(), request)
	if err != nil || repeated.Id != job.Id {
		t.Fatalf("repeat: %v %v", repeated, err)
	}
	request.Arguments[1] = "different"
	if _, err := manager.Start(t.Context(), request); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("conflict: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, job.Id+".json"))
	if strings.Contains(string(data), "hidden-secret") {
		t.Fatal("arguments persisted")
	}
	close(gate)
	var last uint64
	if err := manager.Stream(t.Context(), job.Id, 0, func(event *adminv1.StreamJobResponse) error { last = event.Sequence; return nil }); err != nil {
		t.Fatal(err)
	}
	finished, err := manager.Get(t.Context(), job.Id)
	if err != nil || finished.State != adminv1.JobState_JOB_STATE_SUCCEEDED || calls.Load() != 1 {
		t.Fatalf("completion: %v %v calls=%d", finished, err, calls.Load())
	}
	if err := manager.Stream(t.Context(), job.Id, last, func(*adminv1.StreamJobResponse) error { t.Fatal("replayed delivered event"); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(t.Context(), dir, registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Arguments[1] = "hidden-secret"
	repeated, err = restarted.Start(t.Context(), request)
	if err != nil || repeated.Id != job.Id || calls.Load() != 1 {
		t.Fatalf("restart deduplication: %v %v", repeated, err)
	}
}

func TestJobPermissionsCancellationAndShutdown(t *testing.T) {
	registry := command.NewRegistry()
	registry.Register(command.Command{Name: "work", Run: func(ctx context.Context, _ []string, _ io.Writer) error { <-ctx.Done(); return ctx.Err() }})
	manager, err := New(t.Context(), t.TempDir(), registry, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := controlauth.WithPrincipal(t.Context(), controlauth.Token{ID: "owner", Scopes: []string{"command:work"}})
	stranger := controlauth.WithPrincipal(t.Context(), controlauth.Token{ID: "stranger", Scopes: []string{"read"}})
	job, err := manager.Start(owner, &adminv1.StartJobRequest{Arguments: []string{"work"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Cancel(stranger, job.Id); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("unauthorized cancel: %v", err)
	}
	if _, err := manager.Start(stranger, &adminv1.StartJobRequest{Arguments: []string{"work"}}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("unauthorized start: %v", err)
	}
	if _, err := manager.Cancel(owner, job.Id); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Fatal(err)
	}
	finished, err := manager.Get(owner, job.Id)
	if err != nil || finished.State != adminv1.JobState_JOB_STATE_CANCELLED {
		t.Fatalf("cancel: %v %v", finished, err)
	}
	if _, err := manager.Start(owner, &adminv1.StartJobRequest{Arguments: []string{"work"}}); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("start after close: %v", err)
	}
}

func TestRecoveryMarksUnfinishedJobWithoutRerunning(t *testing.T) {
	dir := t.TempDir()
	id := rand.Text()
	data, _ := json.Marshal(&record{Info: &adminv1.JobInfo{Id: id, Command: "work", State: adminv1.JobState_JOB_STATE_RUNNING}, Owner: "local", Key: "original"})
	if err := os.WriteFile(filepath.Join(dir, id+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".events"), []byte(`{"partial":`), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := New(t.Context(), dir, command.NewRegistry(), nil)
	if err != nil {
		t.Fatal(err)
	}
	job, err := manager.Get(t.Context(), id)
	if err != nil || job.State != adminv1.JobState_JOB_STATE_INTERRUPTED || job.ExitCode == 0 {
		t.Fatalf("recovery: %v %v", job, err)
	}
}
