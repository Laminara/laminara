package api_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
	"github.com/laminara/laminara/gen/go/laminara/api/v1/apiv1connect"
	"github.com/laminara/laminara/server/internal/api"
)

func TestAccountPolicyAvailableBeforeLogin(t *testing.T) {
	for _, tc := range []struct {
		name    string
		policy  *apiv1.AccountPolicy
		enabled bool
		limit   uint32
	}{
		{name: "default", limit: 1},
		{name: "enabled", policy: &apiv1.AccountPolicy{MultiAccount: true, MaxAccounts: 3}, enabled: true, limit: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, handler := apiv1connect.NewLauncherServiceHandler(api.NewService(api.Options{AccountPolicy: tc.policy}))
			server := httptest.NewServer(handler)
			defer server.Close()
			client := apiv1connect.NewLauncherServiceClient(server.Client(), server.URL)
			response, err := client.GetAccountPolicy(context.Background(), connect.NewRequest(&apiv1.GetAccountPolicyRequest{}))
			if err != nil {
				t.Fatal(err)
			}
			if got := response.Msg.Policy; got == nil || got.MultiAccount != tc.enabled || got.MaxAccounts != tc.limit {
				t.Fatalf("unexpected policy: %+v", got)
			}
		})
	}
}
