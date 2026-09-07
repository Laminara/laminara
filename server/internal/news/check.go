package news

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/laminara/laminara/server/internal/diag"
)

var markupPattern = regexp.MustCompile(`(?i)<\s*(a|b|i|p|br|div|span|img|script|strong|em|h[1-6])\b`)

func (f *fileSource) Check(_ context.Context, probe *diag.Probe) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		probe.Fail("файл новостей", fmt.Sprintf("%s: %v", f.path, err), diag.Remedy{
			Hint: "укажите существующий файл в news.source.config.path или уберите блок news",
		})
		return
	}
	items, err := parse(data)
	if err != nil {
		probe.Fail("файл новостей", fmt.Sprintf("%s не разбирается: %v", f.path, err), diag.Remedy{
			Hint: "лаунчер молча покажет прошлые новости, а новые не появятся; ожидается JSON со списком записей",
		})
		return
	}
	reportItems(probe, fmt.Sprintf("%s, новостей %d", f.path, len(items)), items)
}

func (h *httpSource) Check(ctx context.Context, probe *diag.Probe) {
	started := time.Now()
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	items, err := h.Items(callCtx)
	if err != nil {
		probe.Fail("источник новостей", fmt.Sprintf("%s не отвечает: %v", h.url, err), diag.Remedy{
			Hint: "лаунчер покажет пустую ленту; проверьте адрес и доступность API",
		})
		return
	}
	reportItems(probe, fmt.Sprintf("%s отвечает за %s, новостей %d", h.url, time.Since(started).Round(time.Millisecond), len(items)), items)
}

func reportItems(probe *diag.Probe, detail string, items []Item) {
	if len(items) == 0 {
		probe.Warn("новости", detail, diag.Remedy{
			Hint: "лента пуста — в лаунчере блок новостей будет пустым",
		})
		return
	}
	for _, item := range items {
		if markupPattern.MatchString(item.Body) || markupPattern.MatchString(item.Title) {
			probe.Warn("новости", fmt.Sprintf("%s; в записи %q есть разметка", detail, item.Title), diag.Remedy{
				Hint: "лаунчер показывает новости обычным текстом и теги выведет как есть — уберите разметку из текста",
			})
			return
		}
	}
	probe.OK("новости", "%s", detail)
}
