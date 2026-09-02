package modulesetup

import (
	"log/slog"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/module"
	"github.com/laminara/laminara/server/internal/module/builtin"
	"github.com/laminara/laminara/server/internal/module/remote"
)

type Runtime struct {
	Registry *module.Registry
	Loader   *remote.Loader
}

func Build(cfg *config.ModulesConfig, log *slog.Logger) *Runtime {
	registry := module.NewRegistry()
	registry.Add(builtin.NewDiagnostics(registry))
	runtime := &Runtime{Registry: registry}
	if cfg == nil || cfg.Dir == "" {
		return runtime
	}
	configs := make(map[string][]byte, len(cfg.Config))
	for name, raw := range cfg.Config {
		configs[name] = raw
	}
	runtime.Loader = remote.NewLoader(log)
	if err := runtime.Loader.LoadDir(cfg.Dir, configs, registry); err != nil {
		log.Error("modules dir scan failed", "dir", cfg.Dir, "error", err)
	}
	return runtime
}
