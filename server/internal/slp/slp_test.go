package slp

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestSplitAddress(t *testing.T) {
	cases := map[string]struct {
		host string
		port uint16
	}{
		"play.example.com":       {"play.example.com", 25565},
		"play.example.com:25566": {"play.example.com", 25566},
		"127.0.0.1:25560":        {"127.0.0.1", 25560},
		" mc.example.net ":       {"mc.example.net", 25565},
	}
	for address, want := range cases {
		host, port := SplitAddress(address)
		if host != want.host || port != want.port {
			t.Fatalf("SplitAddress(%q) = %q,%d; want %q,%d", address, host, port, want.host, want.port)
		}
	}
}

func TestDecode(t *testing.T) {
	status, err := decode([]byte(`{"version":{"name":"1.21.1"},"players":{"online":3,"max":60,"sample":[{"name":"neo"},{"name":""},{"name":"trinity"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if status.Online != 3 || status.Max != 60 || status.Version != "1.21.1" {
		t.Fatalf("status = %+v", status)
	}
	if len(status.Sample) != 2 || status.Sample[0] != "neo" || status.Sample[1] != "trinity" {
		t.Fatalf("sample = %v", status.Sample)
	}
}

func TestPingRefusesQuickly(t *testing.T) {
	if _, err := PingContext(context.Background(), "127.0.0.1:1", 300*time.Millisecond); err == nil {
		t.Fatal("a closed port must report an error")
	}
}

func TestPingHonoursContext(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		var held []net.Conn
		defer func() {
			for _, conn := range held {
				_ = conn.Close()
			}
		}()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			held = append(held, conn)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()
	started := time.Now()
	if _, err := PingContext(ctx, listener.Addr().String(), 5*time.Second); err == nil {
		t.Fatal("a cancelled ping must report an error")
	}
	if took := time.Since(started); took > time.Second {
		t.Fatalf("cancellation took %s", took)
	}
}
