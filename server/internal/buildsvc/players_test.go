package buildsvc

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/laminara/laminara/server/internal/admin"
)

func serveStatus(t *testing.T, body string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reply := append([]byte{0x00}, binary.AppendUvarint(nil, uint64(len(body)))...)
		reply = append(reply, body...)
		_, _ = conn.Write(binary.AppendUvarint(nil, uint64(len(reply))))
		_, _ = conn.Write(reply)
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		for {
			if _, err := conn.Read(make([]byte, 512)); err != nil {
				return
			}
		}
	}()
	return listener.Addr().String()
}

func TestPingBuildReadsStatus(t *testing.T) {
	address := serveStatus(t, `{"version":{"name":"1.21.1"},"players":{"online":2,"max":20,"sample":[{"name":"neo"}]}}`)
	entry := &admin.BuildPlayers{Build: "Alpha", Address: address}
	if err := pingBuild(context.Background(), entry, address); err != nil {
		t.Fatal(err)
	}
	if !entry.Reachable || entry.Online != 2 || entry.Max != 20 || entry.Version != "1.21.1" {
		t.Fatalf("entry = %+v", entry)
	}
	if len(entry.Names) != 1 || entry.Names[0] != "neo" {
		t.Fatalf("names = %v", entry.Names)
	}
}

func TestPingBuildKeepsDialError(t *testing.T) {
	entry := &admin.BuildPlayers{Build: "Alpha", Address: "127.0.0.1:1"}
	if err := pingBuild(context.Background(), entry, "127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	if entry.Reachable || !strings.Contains(entry.Error, "connect") {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestPingBuildStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pingBuild(ctx, &admin.BuildPlayers{}, "10.255.255.1:25565"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
