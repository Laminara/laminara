package doctor

import (
	"context"
	"sort"
	"time"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/diag"
	"github.com/laminara/laminara/server/internal/progress"
	"github.com/laminara/laminara/server/internal/serversetup"
)

type Options struct {
	Config     *config.Config
	ConfigPath string
	Wired      *serversetup.Wired
	BuildError error
	Running    bool
	Username   string
	Password   string
	Deep       bool
	Endpoint   string
}

type section struct {
	key   string
	title string
	run   func(ctx context.Context, opts Options, probe *diag.Probe)
}

var sections = []section{
	{key: "config", title: "настройки", run: checkConfigFile},
	{key: "auth", title: "аккаунты и вход", run: checkAuth},
	{key: "storage", title: "хранилище файлов", run: checkStorage},
	{key: "build", title: "сборки и подпись", run: checkBuild},
	{key: "api", title: "публичный доступ", run: checkAPI},
	{key: "yggdrasil", title: "вход в игре", run: checkYggdrasil},
	{key: "hwid", title: "распознавание компьютеров", run: checkMachines},
	{key: "access", title: "доступ к сборкам", run: checkAccess},
	{key: "news", title: "новости", run: checkNews},
	{key: "console", title: "консоль в браузере", run: checkConsole},
	{key: "launcher", title: "лаунчер", run: checkLauncher},
	{key: "modules", title: "модули", run: checkModules},
	{key: "rateLimit", title: "защита от перебора", run: checkRateLimit},
	{key: "update", title: "обновления", run: checkUpdate},
	{key: "log", title: "журнал", run: checkLog},
	{key: "crashReports", title: "отчёты о падениях", run: checkCrashes},
	{key: "branding", title: "оформление", run: checkBranding},
	{key: "flow", title: "путь игрока", run: checkFlow},
	{key: "consistency", title: "связность настроек", run: checkConsistency},
}

const sectionTimeout = 45 * time.Second

func Titles() map[string]string {
	titles := make(map[string]string, len(sections))
	for _, s := range sections {
		titles[s.key] = s.title
	}
	return titles
}

func Sections() []string {
	keys := make([]string, 0, len(sections))
	for _, s := range sections {
		keys = append(keys, s.key)
	}
	return keys
}

func Run(ctx context.Context, opts Options) []diag.Result {
	return RunSections(ctx, opts, nil)
}

func RunSections(ctx context.Context, opts Options, only []string) []diag.Result {
	wanted := map[string]bool{}
	for _, name := range only {
		wanted[name] = true
	}
	var results []diag.Result
	if opts.BuildError != nil && (len(wanted) == 0 || !wanted["config"]) {
		probe := diag.New("config")
		reportBuildError(opts, probe)
		results = append(results, probe.Results()...)
	}
	planned := 0
	for _, s := range sections {
		if len(wanted) == 0 || wanted[s.key] {
			planned++
		}
	}
	done := 0
	for _, s := range sections {
		if len(wanted) > 0 && !wanted[s.key] {
			continue
		}
		progress.Report(ctx, progress.Event{Phase: s.title, Current: int64(done), Total: int64(planned)})
		probe := diag.New(s.key)
		sectionCtx, cancel := context.WithTimeout(ctx, sectionTimeout)
		s.run(sectionCtx, opts, probe)
		cancel()
		results = append(results, probe.Results()...)
		done++
	}
	progress.Report(ctx, progress.Event{Phase: "проверка закончена", Current: int64(done), Total: int64(planned)})
	return results
}

func order(results []diag.Result) []diag.Result {
	index := map[string]int{}
	for i, s := range sections {
		index[s.key] = i
	}
	sorted := make([]diag.Result, len(results))
	copy(sorted, results)
	sort.SliceStable(sorted, func(i, j int) bool {
		return index[sorted[i].Section] < index[sorted[j].Section]
	})
	return sorted
}

func Worst(results []diag.Result) diag.Verdict {
	return diag.Worst(results)
}
