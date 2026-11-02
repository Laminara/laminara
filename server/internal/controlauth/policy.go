package controlauth

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"github.com/laminara/laminara/server/internal/command"
)

type principalKey struct{}

func Principal(ctx context.Context) Token {
	if token, ok := ctx.Value(principalKey{}).(Token); ok {
		return token
	}
	return Token{ID: "local", Name: "local", Scopes: []string{"admin"}}
}

func WithPrincipal(ctx context.Context, token Token) context.Context {
	return command.WithPolicy(context.WithValue(ctx, principalKey{}, token), policy{token: token})
}

func (t Token) Allows(scope string) bool {
	return slices.Contains(t.Scopes, "admin") || slices.Contains(t.Scopes, scope)
}

type policy struct{ token Token }

func (p policy) Authorize(_ context.Context, cmd command.Command) error {
	if p.token.Allows("command:"+cmd.Name) || p.token.Allows(commandScope(cmd.Name)) {
		return nil
	}
	slog.Warn("административная команда запрещена", "source", "audit", "actor", p.token.ID, "command", cmd.Name)
	return errors.New("токен не разрешает эту команду")
}

func (p policy) Completed(_ context.Context, cmd command.Command, err error) {
	slog.Info("административная команда завершена", "source", "audit", "actor", p.token.ID, "command", cmd.Name, "success", err == nil)
}

func commandScope(name string) string {
	switch name {
	case "help", "status", "version", "versions", "loaders", "builds", "build", "players":
		return "read"
	case "install", "publish", "delete", "launcher", "build-apply", "build-rollback":
		return "builds"
	case "auth":
		return "accounts"
	case "access", "hwid", "machines", "ban", "bans":
		return "access"
	case "signing":
		return "signing"
	case "settings":
		return "settings"
	case "restart", "stop", "update", "doctor":
		return "lifecycle"
	case "modules":
		return "modules"
	default:
		return "admin"
	}
}

func rpcScope(method string) string {
	switch method {
	case "Status", "ListCommands", "ListVersions", "ListLoaders", "ListBuilds", "ListPlayers", "ListSettings", "PreviewSettings", "PreviewBuildSettings":
		return "read"
	case "StreamLogs":
		return "logs"
	case "ApplySettings", "SetSetting", "AddSettingEntry", "RemoveSettingEntry":
		return "settings"
	case "ApplyBuildSettings", "PlanBuild", "UploadBuildChunk":
		return "builds"
	case "Restart", "Stop":
		return "lifecycle"
	case "Exec", "StartJob", "GetJob", "ListJobs", "CancelJob", "StreamJob":
		return ""
	default:
		return "admin"
	}
}
