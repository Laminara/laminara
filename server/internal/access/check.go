package access

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/laminara/laminara/server/internal/diag"
)

func (f *fileSource) Check(_ context.Context, probe *diag.Probe) {
	if _, err := os.Stat(f.path); err != nil {
		probe.Fail("список допущенных", fmt.Sprintf("%s: %v", f.path, err), diag.Remedy{
			Hint: "пока файла нет, к закрытым сборкам не пустят никого; создайте его или уберите правило доступа",
		})
		return
	}
	roster, err := f.load()
	if err != nil {
		probe.Fail("список допущенных", fmt.Sprintf("%s не разбирается: %v", f.path, err), diag.Remedy{
			Hint: "ожидается JSON вида {\"users\":[\"ник\"]} или список ников",
		})
		return
	}
	total := len(roster.global)
	for _, members := range roster.perBuild {
		total += len(members)
	}
	if total == 0 {
		probe.Warn("список допущенных", fmt.Sprintf("%s пуст — к закрытым сборкам не пустят никого", f.path), diag.Remedy{
			Hint: "добавьте ников в файл или снимите правило доступа со сборки",
		})
		return
	}
	probe.OK("список допущенных", "%s, записей %d", f.path, total)
}

func (h *httpSource) Check(ctx context.Context, probe *diag.Probe) {
	started := time.Now()
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := h.Allows(callCtx, "laminara-doctor", Subject{Username: "laminara-doctor"})
	if err != nil {
		if h.failOpen {
			probe.Warn("источник доступа", fmt.Sprintf("%s не отвечает: %v", h.url, err), diag.Remedy{
				Hint: "включён failOpen, поэтому пока источник молчит, к закрытым сборкам пускают всех",
			})
			return
		}
		probe.Fail("источник доступа", fmt.Sprintf("%s не отвечает: %v", h.url, err), diag.Remedy{
			Hint: "пока источник молчит, к закрытым сборкам не пустят никого; проверьте адрес, заголовки и доступность API",
		})
		return
	}
	probe.OK("источник доступа", "%s отвечает за %s", h.url, time.Since(started).Round(time.Millisecond))
}
