package cli

import (
	"context"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
)

func TestProgressSmoke(t *testing.T) {
	if os.Getenv("LAMINARA_PROGRESS_SMOKE") == "" {
		t.Skip("set LAMINARA_PROGRESS_SMOKE=1 with a running daemon (starts a real install, cancels early)")
	}
	client := adminClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	line := os.Getenv("LAMINARA_PROGRESS_CMD")
	if line == "" {
		line = "install _progresstest 1.21.1"
	}
	stream, err := client.Exec(ctx, connect.NewRequest(&adminv1.ExecRequest{Line: line}))
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for stream.Receive() {
		if p := stream.Msg().GetProgress(); p != nil {
			t.Logf("progress: phase=%-24q %d/%d %s", p.Phase, p.Current, p.Total, p.Message)
			if seen++; seen >= 10 {
				cancel()
				break
			}
		}
	}
	if seen == 0 {
		t.Fatal("no progress events received")
	}
}

func TestRPCSmoke(t *testing.T) {
	if os.Getenv("LAMINARA_RPC_SMOKE") == "" {
		t.Skip("set LAMINARA_RPC_SMOKE=1 with a running daemon")
	}
	client := adminClient()
	ctx := context.Background()

	versions, err := client.ListVersions(ctx, connect.NewRequest(&adminv1.ListVersionsRequest{Query: "1.21"}))
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	t.Logf("ListVersions: %d versions, latest release %s", len(versions.Msg.Versions), versions.Msg.LatestRelease)
	if len(versions.Msg.Versions) == 0 {
		t.Fatal("no versions")
	}

	loaders, err := client.ListLoaders(ctx, connect.NewRequest(&adminv1.ListLoadersRequest{McVersion: "1.21.1"}))
	if err != nil {
		t.Fatalf("ListLoaders: %v", err)
	}
	names := make([]string, 0, len(loaders.Msg.Loaders))
	for _, l := range loaders.Msg.Loaders {
		names = append(names, l.Name)
	}
	t.Logf("ListLoaders(1.21.1): %v", names)

	builds, err := client.ListBuilds(ctx, connect.NewRequest(&adminv1.ListBuildsRequest{}))
	if err != nil {
		t.Fatalf("ListBuilds: %v", err)
	}
	t.Logf("ListBuilds: %d", len(builds.Msg.Builds))
}
