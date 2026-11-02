package admin

import (
	"context"
	"io"
	"net/http/httptest"
	"reflect"
	"testing"

	"connectrpc.com/connect"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
	"github.com/laminara/laminara/server/internal/command"
)

func TestExecPreservesStructuredAndLegacyArguments(t *testing.T) {
	for _, test := range []struct {
		name    string
		request *adminv1.ExecRequest
		want    []string
	}{
		{"structured", &adminv1.ExecRequest{Arguments: []string{"echo", "My Pack", "", `C:\new\mods`, `{"label":"two  spaces"}`}}, []string{"My Pack", "", `C:\new\mods`, `{"label":"two  spaces"}`}},
		{"legacy", &adminv1.ExecRequest{Line: `e "My Pack"`}, []string{"My Pack"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry := command.NewRegistry()
			var got []string
			registry.Register(command.Command{Name: "echo", Aliases: []string{"e"}, Run: func(_ context.Context, args []string, _ io.Writer) error { got = args; return nil }})
			_, handler := adminv1connect.NewAdminServiceHandler(NewService(nil, nil, registry, nil))
			server := httptest.NewServer(handler)
			defer server.Close()
			client := adminv1connect.NewAdminServiceClient(server.Client(), server.URL)
			stream, err := client.Exec(context.Background(), connect.NewRequest(test.request))
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			completed := false
			for stream.Receive() {
				if result := stream.Msg().GetResult(); result != nil {
					completed = result.ExitCode == 0
				}
			}
			if stream.Err() != nil || !completed || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("Exec: got %q completed=%v err=%v", got, completed, stream.Err())
			}
		})
	}
}

func TestExecRejectsAmbiguousRequest(t *testing.T) {
	registry := command.NewRegistry()
	called := false
	registry.Register(command.Command{Name: "delete", Run: func(context.Context, []string, io.Writer) error { called = true; return nil }})
	_, handler := adminv1connect.NewAdminServiceHandler(NewService(nil, nil, registry, nil))
	server := httptest.NewServer(handler)
	defer server.Close()
	client := adminv1connect.NewAdminServiceClient(server.Client(), server.URL)
	stream, err := client.Exec(context.Background(), connect.NewRequest(&adminv1.ExecRequest{Line: "delete small", Arguments: []string{"delete", "My Pack"}}))
	if err == nil {
		defer stream.Close()
		for stream.Receive() {
		}
		err = stream.Err()
	}
	if called || connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("ambiguous request: called=%v err=%v", called, err)
	}
}
