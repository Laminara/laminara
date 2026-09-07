package redisstore

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/diag"
)

func (s *SessionStore) Check(ctx context.Context, probe *diag.Probe) {
	started := time.Now()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.client.Ping(pingCtx).Err(); err != nil {
		probe.Fail("сессии", fmt.Sprintf("redis не отвечает: %v", err), diag.Remedy{
			Hint: "проверьте auth.sessions.redisAddr и то, что redis запущен; без него никто не войдёт",
		})
		return
	}
	probe.OK("сессии", "redis отвечает за %s", time.Since(started).Round(time.Millisecond))

	session := &auth.Session{ID: uuid.New(), Identity: auth.Identity{Subject: "laminara-doctor"}}
	if err := s.Create(ctx, session); err != nil {
		probe.Fail("запись сессии", fmt.Sprintf("не сохраняется: %v", err), diag.Remedy{
			Hint: "redis отвечает, но не принимает запись — проверьте права пользователя и что база не в режиме только для чтения",
		})
		return
	}
	defer func() {
		if err := s.Revoke(context.WithoutCancel(ctx), session.ID); err != nil {
			probe.Warn("уборка пробы", fmt.Sprintf("пробная сессия не удалилась: %v", err), diag.Remedy{
				Hint: "она исчезнет сама по истечении срока жизни сессии",
			})
		}
	}()
	stored, err := s.Get(ctx, session.ID)
	if err != nil {
		probe.Fail("запись сессии", fmt.Sprintf("записанная сессия не читается: %v", err), diag.Remedy{
			Hint: "redis принимает запись, но не отдаёт её обратно — проверьте вытеснение ключей (maxmemory-policy)",
		})
		return
	}
	if stored == nil || stored.ID != session.ID {
		probe.Fail("запись сессии", "прочиталась не та сессия", diag.Remedy{
			Hint: "похоже, этот redis делят с другим приложением и ключи затираются",
		})
		return
	}
	probe.OK("запись сессии", "запись и чтение проходят")
}
