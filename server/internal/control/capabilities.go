package control

import (
	"context"
	"fmt"
	"slices"

	"connectrpc.com/connect"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
)

func RequireCapability(ctx context.Context, client adminv1connect.AdminServiceClient, capability adminv1.AdminCapability) error {
	response, err := client.Status(ctx, connect.NewRequest(&adminv1.StatusRequest{}))
	if err != nil {
		return fmt.Errorf("проверить возможности запущенного сервера: %w", err)
	}
	if !slices.Contains(response.Msg.Capabilities, capability) {
		return fmt.Errorf("запущенный сервер не поддерживает эту операцию клиента; обновите и перезапустите сервер")
	}
	return nil
}
