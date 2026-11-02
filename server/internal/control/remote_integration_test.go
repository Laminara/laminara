package control_test

import (
	"context"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
	"github.com/laminara/laminara/server/internal/admin"
	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/control"
	"github.com/laminara/laminara/server/internal/controlauth"
	"github.com/laminara/laminara/server/internal/jobs"
)

func TestTLSRemoteJobsReconnectAndPermissions(t *testing.T) {
	registry := command.NewRegistry()
	gate := make(chan struct{})
	var calls atomic.Int32
	registry.Register(command.Command{Name: "install", Aliases: []string{"prepare"}, Run: func(ctx context.Context, _ []string, out io.Writer) error {
		calls.Add(1)
		if _, err := io.WriteString(out, "first\n"); err != nil {
			return err
		}
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
		_, err := io.WriteString(out, "last\n")
		return err
	}})
	tokens := controlauth.NewStore(filepath.Join(t.TempDir(), "tokens.json"))
	_, raw, err := tokens.Create("ci", []string{"read", "builds"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := jobs.New(t.Context(), t.TempDir(), registry, tokens.Active)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close(context.Background())
	service := admin.NewService(nil, nil, registry, nil)
	service.SetJobs(manager)
	mux := http.NewServeMux()
	mux.Handle(adminv1connect.NewAdminServiceHandler(service))
	server := httptest.NewTLSServer(tokens.Handler(mux))
	defer server.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := control.NewClient(control.ClientOptions{URL: server.URL, Token: raw, CAFile: ca})
	if err != nil {
		t.Fatal(err)
	}
	request := &adminv1.StartJobRequest{Arguments: []string{"prepare", "pack"}, IdempotencyKey: "pipeline"}
	started, err := client.StartJob(t.Context(), connect.NewRequest(request))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	stream, err := client.StreamJob(ctx, connect.NewRequest(&adminv1.StreamJobRequest{Id: started.Msg.Job.Id}))
	if err != nil {
		t.Fatal(err)
	}
	if !stream.Receive() {
		t.Fatalf("first event: %v", stream.Err())
	}
	cursor := stream.Msg().Sequence
	cancel()
	stream.Close()
	close(gate)
	repeated, err := client.StartJob(t.Context(), connect.NewRequest(request))
	if err != nil || repeated.Msg.Job.Id != started.Msg.Job.Id {
		t.Fatalf("retry: %v", err)
	}
	var output strings.Builder
	job, err := control.WatchJob(t.Context(), client, started.Msg.Job.Id, cursor, func(event *adminv1.StreamJobResponse) error {
		if chunk := event.Event.GetOutput(); chunk != nil {
			output.WriteString(chunk.Text)
		}
		return nil
	})
	if err != nil || job.State != adminv1.JobState_JOB_STATE_SUCCEEDED || calls.Load() != 1 || output.String() != "last\n" {
		t.Fatalf("reconnect: %v %v calls=%d output=%q", job, err, calls.Load(), output.String())
	}
	_, readerRaw, err := tokens.Create("reader", []string{"read"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := control.NewClient(control.ClientOptions{URL: server.URL, Token: readerRaw, CAFile: ca})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.StartJob(t.Context(), connect.NewRequest(request)); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("reader alias mutation: %v", err)
	}
	if _, err := reader.GetJob(t.Context(), connect.NewRequest(&adminv1.GetJobRequest{Id: job.Id})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("reader private log: %v", err)
	}
	untrusted, err := control.NewClient(control.ClientOptions{URL: server.URL, Token: raw})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := untrusted.Status(t.Context(), connect.NewRequest(&adminv1.StatusRequest{})); err == nil {
		t.Fatal("untrusted TLS certificate accepted")
	}
}
