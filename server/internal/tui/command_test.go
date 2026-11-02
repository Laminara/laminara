package tui

import (
	"context"
	"io"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"

	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
	"github.com/laminara/laminara/server/internal/admin"
	"github.com/laminara/laminara/server/internal/command"
)

type protocolServer struct {
	adminv1connect.UnimplementedAdminServiceHandler
	capabilities []adminv1.AdminCapability
	executed     bool
	written      bool
}

func (s *protocolServer) Status(context.Context, *connect.Request[adminv1.StatusRequest]) (*connect.Response[adminv1.StatusResponse], error) {
	return connect.NewResponse(&adminv1.StatusResponse{Capabilities: s.capabilities}), nil
}

func (s *protocolServer) Exec(context.Context, *connect.Request[adminv1.ExecRequest], *connect.ServerStream[adminv1.ExecResponse]) error {
	s.executed = true
	return nil
}

func (s *protocolServer) SetSetting(context.Context, *connect.Request[adminv1.SetSettingRequest]) (*connect.Response[adminv1.SetSettingResponse], error) {
	s.written = true
	return connect.NewResponse(&adminv1.SetSettingResponse{}), nil
}

func TestOlderDaemonCannotSilentlyAcceptNewOperations(t *testing.T) {
	service := &protocolServer{}
	_, handler := adminv1connect.NewAdminServiceHandler(service)
	server := httptest.NewServer(handler)
	defer server.Close()
	client := adminv1connect.NewAdminServiceClient(server.Client(), server.URL)
	result := runCommand(context.Background(), client, `publish "My Pack"`, make(chan string, 8), make(chan progressMsg, 8), newStyles(), time.Now())().(execDoneMsg)
	if result.err == nil || service.executed || !strings.Contains(result.err.Error(), "перезапустите") {
		t.Fatalf("old daemon accepted structured command: %+v", result)
	}
	saved := setSetting(context.Background(), client, "auth.config.headers", `{"Authorization":"***"}`, adminv1.SettingWriteMode_SETTING_WRITE_MODE_EDIT_REDACTED)().(settingsSavedMsg)
	if saved.err == nil || service.written {
		t.Fatal("old daemon accepted masked setting")
	}
}

func TestCommandWithoutTerminalResultIsNotSuccess(t *testing.T) {
	service := &protocolServer{capabilities: []adminv1.AdminCapability{adminv1.AdminCapability_ADMIN_CAPABILITY_STRUCTURED_ARGUMENTS}}
	_, handler := adminv1connect.NewAdminServiceHandler(service)
	server := httptest.NewServer(handler)
	defer server.Close()
	client := adminv1connect.NewAdminServiceClient(server.Client(), server.URL)
	result := runCommand(context.Background(), client, "publish pack", make(chan string, 8), make(chan progressMsg, 8), newStyles(), time.Now())().(execDoneMsg)
	if result.err == nil || !service.executed || !strings.Contains(result.err.Error(), "без результата") {
		t.Fatalf("incomplete stream counted as success: %+v", result)
	}
}

func TestGeneratedBuildActionsPreserveTheSelectedBuild(t *testing.T) {
	name := `Моя сборка "с модами"`
	w := freshWizard(nil)
	w.mc = "1.21.1"
	w.name.SetValue(name)
	for _, line := range []string{w.buildCommand(), command.Format("publish", name), deleteConfirm(name, unicodeIcons()).command} {
		registry := command.NewRegistry()
		var got []string
		arguments, err := command.Split(line)
		if err != nil {
			t.Fatal(err)
		}
		registry.Register(command.Command{Name: arguments[0], Run: func(_ context.Context, args []string, _ io.Writer) error { got = args; return nil }})
		_, handler := adminv1connect.NewAdminServiceHandler(admin.NewService(nil, nil, registry, nil))
		server := httptest.NewServer(handler)
		client := adminv1connect.NewAdminServiceClient(server.Client(), server.URL)
		result := runCommand(context.Background(), client, line, make(chan string, 8), make(chan progressMsg, 8), newStyles(), time.Now())().(execDoneMsg)
		server.Close()
		if result.err != nil || result.exitCode != 0 || !reflect.DeepEqual(got, arguments[1:]) {
			t.Fatalf("%s: got %q result=%+v", line, got, result)
		}
	}
}
