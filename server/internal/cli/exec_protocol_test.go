package cli

import (
	"context"
	"net/http/httptest"
	"reflect"
	"testing"

	"connectrpc.com/connect"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
)

type execProtocolServer struct {
	adminv1connect.UnimplementedAdminServiceHandler
	supported bool
	complete  bool
	arguments []string
}

func (s *execProtocolServer) Status(context.Context, *connect.Request[adminv1.StatusRequest]) (*connect.Response[adminv1.StatusResponse], error) {
	response := &adminv1.StatusResponse{}
	if s.supported {
		response.Capabilities = []adminv1.AdminCapability{adminv1.AdminCapability_ADMIN_CAPABILITY_STRUCTURED_ARGUMENTS}
	}
	return connect.NewResponse(response), nil
}

func (s *execProtocolServer) Exec(_ context.Context, request *connect.Request[adminv1.ExecRequest], stream *connect.ServerStream[adminv1.ExecResponse]) error {
	s.arguments = request.Msg.Arguments
	if !s.complete {
		return nil
	}
	return stream.Send(&adminv1.ExecResponse{Event: &adminv1.ExecResponse_Result{Result: &adminv1.ExecResult{}}})
}

func TestExecProtocolPreservesArgumentsAndRequiresCompletion(t *testing.T) {
	for _, test := range []struct {
		name      string
		request   *adminv1.ExecRequest
		supported bool
		complete  bool
		want      []string
		wantError bool
	}{
		{"quoted line", &adminv1.ExecRequest{Line: `publish "My Pack"`}, true, true, []string{"publish", "My Pack"}, false},
		{"structured settings", &adminv1.ExecRequest{Arguments: []string{"settings", "modules.config.custom", `{"text":"два  пробела"}`}}, true, true, []string{"settings", "modules.config.custom", `{"text":"два  пробела"}`}, false},
		{"old daemon", &adminv1.ExecRequest{Arguments: []string{"publish", "My Pack"}}, false, true, nil, true},
		{"old daemon quoted line", &adminv1.ExecRequest{Line: `publish "My Pack"`}, false, true, nil, true},
		{"missing result", &adminv1.ExecRequest{Line: "publish pack"}, true, false, []string{"publish", "pack"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &execProtocolServer{supported: test.supported, complete: test.complete}
			_, handler := adminv1connect.NewAdminServiceHandler(service)
			server := httptest.NewServer(handler)
			defer server.Close()
			client := adminv1connect.NewAdminServiceClient(server.Client(), server.URL)
			_, err := runExecRequest(context.Background(), client, test.request)
			if (err != nil) != test.wantError || !reflect.DeepEqual(service.arguments, test.want) {
				t.Fatalf("arguments=%q err=%v", service.arguments, err)
			}
		})
	}
}
