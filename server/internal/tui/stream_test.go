package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
)

type sleepingServer struct {
	adminv1connect.AdminServiceClient
	asked chan struct{}
}

func (s *sleepingServer) Status(context.Context, *connect.Request[adminv1.StatusRequest]) (*connect.Response[adminv1.StatusResponse], error) {
	select {
	case s.asked <- struct{}{}:
	default:
	}
	return nil, errors.New("connection refused")
}

func TestConsoleSaysItLostTheServerOnlyOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	logCh := make(chan string, 64)
	client := &sleepingServer{asked: make(chan struct{}, 1)}
	done := make(chan struct{})
	go func() {
		streamLogs(ctx, client, logCh, newStyles())
		close(done)
	}()

	<-done
	close(logCh)

	var said []string
	for line := range logCh {
		said = append(said, line)
	}
	if len(said) != 1 {
		t.Fatalf("за три секунды без сервера консоль написала %d строк вместо одной:\n%s", len(said), strings.Join(said, "\n"))
	}
	if !strings.Contains(said[0], "связь с проектом прервалась") {
		t.Fatalf("сказано не то: %q", said[0])
	}
}

type wakingServer struct {
	adminv1connect.AdminServiceClient
	calls int
}

func (s *wakingServer) StreamLogs(context.Context, *connect.Request[adminv1.StreamLogsRequest]) (*connect.ServerStreamForClient[adminv1.StreamLogsResponse], error) {
	return nil, errors.New("поток не открылся")
}

func (s *wakingServer) Status(context.Context, *connect.Request[adminv1.StatusRequest]) (*connect.Response[adminv1.StatusResponse], error) {
	s.calls++
	if s.calls <= 2 {
		return nil, errors.New("connection refused")
	}
	return connect.NewResponse(&adminv1.StatusResponse{Version: "тест"}), nil
}

func TestConsoleGreetsTheServerOnlyAfterItAnswers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	logCh := make(chan string, 64)
	done := make(chan struct{})
	go func() {
		streamLogs(ctx, &wakingServer{}, logCh, newStyles())
		close(done)
	}()

	<-done
	close(logCh)

	var lost, back int
	for line := range logCh {
		switch {
		case strings.Contains(line, "связь с проектом прервалась"):
			lost++
		case strings.Contains(line, "проект снова на связи"):
			back++
		}
	}
	if lost != 1 {
		t.Errorf("о потере связи сказано %d раз вместо одного", lost)
	}
	if back != 1 {
		t.Errorf("о возвращении связи сказано %d раз вместо одного — так и появлялась лента из чередующихся строк", back)
	}
}
