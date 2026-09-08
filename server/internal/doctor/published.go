package doctor

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"path/filepath"

	"google.golang.org/protobuf/proto"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/authlib"
	"github.com/laminara/laminara/server/internal/catalog"
	"github.com/laminara/laminara/server/internal/diag"
	"github.com/laminara/laminara/server/internal/humanize"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/storage"
)

const sampledObjects = 12

func checkPublished(ctx context.Context, opts Options, probe *diag.Probe) {
	wired := opts.Wired
	if wired == nil || wired.Catalog == nil {
		return
	}
	summaries, err := wired.Catalog.Summaries(corev1.Platform_PLATFORM_UNSPECIFIED)
	if err != nil {
		probe.Warn("опубликованные сборки", fmt.Sprintf("список не читается: %v", err), diag.Remedy{
			Hint: "проверьте права на каталог сборок",
		})
		return
	}
	names := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		names = append(names, summary.Name)
	}
	if len(names) == 0 {
		probe.Warn("опубликованные сборки", "ни одной — лаунчеру нечего показывать", diag.Remedy{
			Hint:    "подготовьте клиент и опубликуйте его",
			Command: "laminara-server exec \"install survival 1.21.1 loader=neoforge\"",
		})
		return
	}
	probe.OK("опубликованные сборки", "%s", humanize.Count(len(names), "сборка", "сборки", "сборок"))

	broken := 0
	for _, summary := range summaries {
		for _, target := range platformsOf(summary) {
			if !checkManifest(ctx, opts, probe, summary.Name, target) {
				broken++
			}
		}
		checkAuthlib(opts, probe, summary.Name)
	}
	if broken == 0 {
		probe.OK("файлы сборок", "на месте")
	}
}

func platformsOf(summary catalog.Summary) []corev1.Platform {
	if len(summary.Platforms) == 0 {
		return []corev1.Platform{corev1.Platform_PLATFORM_UNSPECIFIED}
	}
	return summary.Platforms
}

func checkManifest(ctx context.Context, opts Options, probe *diag.Probe, name string, target corev1.Platform) bool {
	wired := opts.Wired
	canonical, signature, err := wired.Catalog.Get(name, target)
	if err != nil {
		probe.Fail("сборка "+name, fmt.Sprintf("манифест не читается: %v", err), diag.Remedy{
			Hint: "опубликуйте сборку заново",
		})
		return false
	}
	if wired.Signing != nil {
		public, ok := wired.Signing.Active().Public().(ed25519.PublicKey)
		if ok && !manifest.Verify(public, canonical, signature) {
			probe.Fail("сборка "+name, "подписана не текущим ключом", diag.Remedy{
				Hint:    "лаунчер примет её, только если старый ключ остался в кольце доверия; иначе опубликуйте сборку заново",
				Command: fmt.Sprintf("laminara-server exec \"publish %s\"", name),
			})
			return false
		}
	}
	var parsed corev1.Manifest
	if err := proto.Unmarshal(canonical, &parsed); err != nil {
		probe.Fail("сборка "+name, fmt.Sprintf("манифест не разбирается: %v", err), diag.Remedy{
			Hint: "опубликуйте сборку заново",
		})
		return false
	}
	return checkObjects(ctx, opts, probe, name, &parsed)
}

func checkObjects(ctx context.Context, opts Options, probe *diag.Probe, name string, parsed *corev1.Manifest) bool {
	backend := opts.Wired.Storage
	if backend == nil || len(parsed.Files) == 0 {
		return true
	}
	step := 1
	if !opts.Deep && len(parsed.Files) > sampledObjects {
		step = len(parsed.Files) / sampledObjects
	}
	checked := 0
	for i := 0; i < len(parsed.Files); i += step {
		file := parsed.Files[i]
		if file.Object == nil || file.Object.Hash == nil {
			continue
		}
		checked++
		_, exists, err := backend.Stat(ctx, storage.ObjectKey(file.Object.Hash.Algo, file.Object.Hash.Value))
		if err != nil {
			probe.Warn("сборка "+name, fmt.Sprintf("хранилище не отвечает про %s: %v", file.Path, err), diag.Remedy{
				Hint: "проверьте раздел хранилища выше",
			})
			return false
		}
		if !exists {
			probe.Fail("сборка "+name, fmt.Sprintf("в хранилище нет файла %s", file.Path), diag.Remedy{
				Hint:    "игрок будет вечно перекачивать сборку и не сможет запуститься; опубликуйте её заново",
				Command: fmt.Sprintf("laminara-server exec \"publish %s\"", name),
			})
			return false
		}
	}
	if opts.Deep {
		probe.OK("сборка "+name, "все %d файлов на месте, %s", checked, humanize.Bytes(parsed.TotalSize))
		return true
	}
	probe.OK("сборка "+name, "выборка из %d файлов на месте, всего %s", checked, humanize.Bytes(parsed.TotalSize))
	return true
}

func checkAuthlib(opts Options, probe *diag.Probe, name string) {
	if opts.Config.Build == nil || opts.Config.Build.ProfilesDir == "" {
		return
	}
	dir := filepath.Join(opts.Config.Build.ProfilesDir, name)
	if authlib.Present(dir) {
		return
	}
	probe.Fail("вход в игре: "+name, fmt.Sprintf("в сборке нет %s", authlib.FileName), diag.Remedy{
		Hint:    "без него игра не сможет войти на сервер; сервер добавляет этот файл сам при подготовке — пересоберите сборку и опубликуйте заново",
		Command: fmt.Sprintf("laminara-server exec \"install %s\"", name),
	})
}
