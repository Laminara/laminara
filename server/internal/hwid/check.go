package hwid

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/laminara/laminara/server/internal/diag"
)

func (s *SQLStore) Check(ctx context.Context, probe *diag.Probe) {
	started := time.Now()
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.db.PingContext(pingCtx); err != nil {
		probe.Fail("база компьютеров", fmt.Sprintf("не отвечает: %v", err), diag.Remedy{
			Hint: "проверьте hwid.store.config.dsn; пока база молчит, вход игроков будет отклоняться в режиме enforce",
		})
		return
	}
	probe.OK("база компьютеров", "отвечает за %s", time.Since(started).Round(time.Millisecond))

	machine := Machine{
		ID:        "doctor-" + uuid.NewString(),
		ClusterID: "doctor-" + uuid.NewString(),
		Platform:  "doctor",
		FirstSeen: time.Now(),
		LastSeen:  time.Now(),
	}
	if err := s.SaveMachine(ctx, machine, nil); err != nil {
		probe.Fail("запись о компьютере", fmt.Sprintf("не сохраняется: %v", err), diag.Remedy{
			Hint: "база доступна на чтение, но не принимает запись — проверьте права пользователя и что таблицы созданы",
		})
		return
	}
	defer func() {
		if err := s.forget(context.WithoutCancel(ctx), machine.ID); err != nil {
			probe.Warn("уборка пробы", fmt.Sprintf("пробная запись %s не удалилась: %v", machine.ID, err), diag.Remedy{
				Hint: "удалите строку вручную из таблицы hwid_machine",
			})
		}
	}()
	stored, err := s.Machine(ctx, machine.ID)
	if err != nil {
		probe.Fail("запись о компьютере", fmt.Sprintf("записанное не читается: %v", err), diag.Remedy{
			Hint: "проверьте права на чтение таблицы hwid_machine",
		})
		return
	}
	if stored == nil {
		probe.Fail("запись о компьютере", "записанное тут же исчезло", diag.Remedy{
			Hint: "похоже, запись уходит не в ту базу, что читается",
		})
		return
	}
	probe.OK("запись о компьютере", "запись и чтение проходят")

	bans, err := s.ListBans(ctx, false, time.Now())
	if err != nil {
		probe.Warn("баны", fmt.Sprintf("не читаются: %v", err), diag.Remedy{
			Hint: "проверьте таблицу hwid_ban",
		})
		return
	}
	probe.OK("баны", "действующих %d", len(bans))
}

func (m *MemoryStore) Check(_ context.Context, probe *diag.Probe) {
	m.mu.RLock()
	machines := len(m.machines)
	bans := len(m.bans)
	m.mu.RUnlock()
	probe.Warn("база компьютеров", fmt.Sprintf("хранится в памяти: компьютеров %d, банов %d", machines, bans), diag.Remedy{
		Hint:    "после перезапуска все машины и баны забудутся, а забаненные вернутся в игру",
		Command: "laminara-server settings hwid.store.backend sql",
	})
}
