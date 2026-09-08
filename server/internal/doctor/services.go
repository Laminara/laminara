package doctor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/laminara/laminara/server/internal/access"
	"github.com/laminara/laminara/server/internal/crash"
	"github.com/laminara/laminara/server/internal/diag"
	"github.com/laminara/laminara/server/internal/humanize"
	"github.com/laminara/laminara/server/internal/hwid"
	"github.com/laminara/laminara/server/internal/news"
	"github.com/laminara/laminara/server/internal/version"
)

func checkAPI(_ context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.API == nil || cfg.API.Addr == "" {
		probe.Fail("публичный слушатель", "не задан api.addr — лаунчеру некуда обращаться", diag.Remedy{
			Command: "laminara-server settings api.addr 127.0.0.1:8099",
			Hint:    "это адрес, на котором сервер принимает лаунчер; наружу его обычно выставляет nginx",
		})
		return
	}
	addr := cfg.API.Addr
	if opts.Running {
		probe.OK("публичный слушатель", "%s принимает запросы", addr)
	} else {
		listener, err := net.Listen("tcp", addr)
		if err == nil {
			listener.Close()
			probe.OK("публичный слушатель", "%s свободен — сервер сейчас не запущен", addr)
		} else if conn, dialErr := net.DialTimeout("tcp", dialable(addr), 2*time.Second); dialErr == nil {
			conn.Close()
			probe.OK("публичный слушатель", "%s занят — похоже, сервер уже работает", addr)
		} else {
			probe.Fail("публичный слушатель", fmt.Sprintf("%s не занять и не достучаться: %v", addr, err), diag.Remedy{
				Hint: "адрес занят другим приложением или недоступен на этой машине",
			})
		}
	}
	checkProxies(opts, probe)
}

func checkProxies(opts Options, probe *diag.Probe) {
	cfg := opts.Config
	behind := behindProxy(opts)
	trusted := 0
	if cfg.API != nil {
		trusted = len(cfg.API.TrustedProxies)
	}
	switch {
	case behind && trusted == 0:
		probe.Warn("адреса игроков", "сервер стоит за прокси, но api.trustedProxies пуст", diag.Remedy{
			Hint:    "все игроки будут выглядеть одним адресом, и защита от перебора начнёт блокировать вход сразу всем",
			Command: "laminara-server settings api.trustedProxies 127.0.0.1",
		})
	case trusted > 0:
		probe.OK("адреса игроков", "доверенных прокси %d", trusted)
	default:
		probe.OK("адреса игроков", "берутся из соединения напрямую")
	}
}

func behindProxy(opts Options) bool {
	cfg := opts.Config
	if cfg.Console != nil && strings.HasPrefix(cfg.Console.PublicURL, "https://") {
		return true
	}
	if cfg.Launcher != nil {
		for _, endpoint := range cfg.Launcher.Endpoints {
			if strings.HasPrefix(endpoint, "https://") {
				return true
			}
		}
	}
	return false
}

func checkYggdrasil(ctx context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.Yggdrasil == nil || !cfg.Yggdrasil.Enabled {
		probe.Fail("вход в игре", "yggdrasil выключен — войти не сможет никто", diag.Remedy{
			Hint:    "вход в лаунчере не завершается, пока не ответит yggdrasil; это не опция для серверов с игроками",
			Command: "laminara-server settings yggdrasil.enabled true",
		})
		return
	}
	if cfg.Yggdrasil.RSAKeyPath == "" {
		probe.Warn("ключ входа", "не задан yggdrasil.rsaKeyPath", diag.Remedy{
			Hint:    "ключ будет создаваться заново при каждом запуске, и уже вошедшие игроки будут вылетать при заходе на сервер",
			Command: "laminara-server settings yggdrasil.rsaKeyPath /var/lib/laminara/yggdrasil-rsa.pem",
		})
	} else if info, err := os.Stat(cfg.Yggdrasil.RSAKeyPath); err != nil {
		probe.Warn("ключ входа", fmt.Sprintf("%s ещё не создан — появится при первом запуске", cfg.Yggdrasil.RSAKeyPath), diag.Remedy{
			Hint: "после запуска сохраните его вместе с ключом подписи",
		})
	} else {
		probe.OK("ключ входа", "%s на месте", cfg.Yggdrasil.RSAKeyPath)
		if info.Mode().Perm()&0o077 != 0 {
			path := cfg.Yggdrasil.RSAKeyPath
			probe.Warn("права на ключ входа", fmt.Sprintf("%s открыт остальным (%v)", path, info.Mode().Perm()), diag.Remedy{
				Hint:    "этим ключом подписываются ответы о входе игроков",
				Command: fmt.Sprintf("chmod 600 %s", path),
				Apply: func(context.Context) error {
					return os.Chmod(path, 0o600)
				},
			})
		}
	}
	if opts.Wired != nil && opts.Wired.Skins != nil {
		if !inspect(ctx, probe, opts.Wired.Skins) {
			probe.Skip("скины", "провайдер %s не умеет проверять себя", cfg.Yggdrasil.SkinProvider)
		}
	}
}

func checkMachines(ctx context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.HWID == nil || cfg.HWID.Mode == hwid.ModeOff {
		probe.Skip("распознавание компьютеров", "выключено — баны по железу работать не будут")
		return
	}
	probe.OK("режим", "%s", modeWord(cfg.HWID.Mode))
	checkSecret(probe, cfg.HWID.SaltPath, "соль отпечатков", "hwid.saltPath", "hwid.salt",
		"без постоянной соли все компьютеры станут новыми после перезапуска, а баны по железу перестанут срабатывать")
	checkSecret(probe, cfg.HWID.TicketSecretPath, "ключ пропусков", "hwid.ticketSecretPath", "hwid-ticket.key",
		"без постоянного ключа выданные пропуска перестают приниматься после перезапуска")

	if opts.Wired == nil || opts.Wired.Machines == nil {
		probe.Skip("база компьютеров", "компоненты не собраны")
		return
	}
	if !inspect(ctx, probe, opts.Wired.Machines.Store()) {
		probe.Skip("база компьютеров", "хранилище %s не умеет проверять себя", cfg.HWID.Store.Backend)
	}
}

func modeWord(mode hwid.Mode) string {
	switch mode {
	case hwid.ModeEnforce:
		return "не пускать нарушителей"
	case hwid.ModeObserve:
		return "только наблюдение"
	default:
		return string(mode)
	}
}

func checkSecret(probe *diag.Probe, path, what, setting, suggested, why string) {
	if path == "" {
		probe.Warn(what, fmt.Sprintf("не задан %s", setting), diag.Remedy{
			Hint:    why,
			Command: fmt.Sprintf("laminara-server settings %s /var/lib/laminara/%s", setting, suggested),
		})
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		probe.Warn(what, fmt.Sprintf("%s ещё не создан — появится при первом запуске", path), diag.Remedy{
			Hint: why,
		})
		return
	}
	probe.OK(what, "%s на месте", path)
	if info.Mode().Perm()&0o077 != 0 {
		probe.Warn("права на "+what, fmt.Sprintf("%s открыт остальным (%v)", path, info.Mode().Perm()), diag.Remedy{
			Hint:    "зная это значение, можно подделать отпечаток чужого компьютера",
			Command: fmt.Sprintf("chmod 600 %s", path),
			Apply: func(context.Context) error {
				return os.Chmod(path, 0o600)
			},
		})
	}
}

func checkAccess(ctx context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.Access == nil || len(cfg.Access.Rules) == 0 {
		probe.Skip("доступ к сборкам", "правил нет — сборки открыты всем")
		return
	}
	probe.OK("правила", "%s", humanize.Count(len(cfg.Access.Rules), "правило", "правила", "правил"))
	if opts.Wired == nil || opts.Wired.Access == nil {
		probe.Skip("источники", "компоненты не собраны")
		return
	}
	for name, source := range opts.Wired.Access.Sources() {
		sub := diag.New(probe.Section())
		if !inspect(ctx, sub, source) {
			sub.Skip("источник "+name, "не умеет проверять себя")
		}
		for _, result := range sub.Results() {
			result.What = fmt.Sprintf("%s (%s)", result.What, name)
			probe.Add(result)
		}
	}
	checkDeadRules(opts, probe)
}

func checkDeadRules(opts Options, probe *diag.Probe) {
	if opts.Wired == nil || opts.Wired.Catalog == nil {
		return
	}
	names, err := opts.Wired.Catalog.List()
	if err != nil || len(names) == 0 {
		return
	}
	for _, rule := range opts.Config.Access.Rules {
		if matchesAny(rule, names) {
			continue
		}
		probe.Warn("правило доступа", fmt.Sprintf("%s не подходит ни к одной сборке", strings.Join(rule.Builds, ", ")), diag.Remedy{
			Hint: "правило ни на что не влияет — проверьте имена сборок в нём",
		})
	}
}

func matchesAny(rule access.RuleConfig, names []string) bool {
	for _, pattern := range rule.Builds {
		for _, name := range names {
			if ok, err := filepath.Match(pattern, name); err == nil && ok {
				return true
			}
		}
	}
	return false
}

func checkNews(ctx context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.News == nil || cfg.News.Source.Type == "" {
		probe.Skip("новости", "источник не задан — блок новостей в лаунчере пуст")
		return
	}
	known := false
	for _, name := range news.SourceNames() {
		if name == cfg.News.Source.Type {
			known = true
			break
		}
	}
	if !known {
		probe.Fail("новости", fmt.Sprintf("источник %q не зарегистрирован", cfg.News.Source.Type), diag.Remedy{
			Hint: fmt.Sprintf("доступны: %s", strings.Join(news.SourceNames(), ", ")),
		})
		return
	}
	if opts.Wired == nil || opts.Wired.News == nil {
		probe.Skip("новости", "компоненты не собраны")
		return
	}
	if !inspect(ctx, probe, opts.Wired.News.Source()) {
		probe.Skip("новости", "источник %s не умеет проверять себя", cfg.News.Source.Type)
	}
}

func checkConsole(ctx context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.Console != nil && !cfg.Console.On() {
		probe.Skip("консоль в браузере", "выключена")
		return
	}
	if cfg.Console == nil || cfg.Console.PublicURL == "" {
		probe.Warn("адрес консоли", "не задан console.publicUrl", diag.Remedy{
			Hint:    "ссылки на вход будут выдаваться без домена, и открыть их снаружи не выйдет",
			Command: "laminara-server settings console.publicUrl https://launcher.example.com",
		})
	} else {
		checkConsoleReach(ctx, opts, probe)
	}
	if cfg.Console != nil && cfg.Console.StatePath != "" {
		dir := filepath.Dir(cfg.Console.StatePath)
		if err := writable(dir); err != nil {
			probe.Warn("сеансы консоли", fmt.Sprintf("%s: %v", cfg.Console.StatePath, err), diag.Remedy{
				Hint:    "без файла сеансов вход в браузере будет забываться при каждом перезапуске",
				Command: fmt.Sprintf("mkdir -p %s", dir),
				Apply: func(context.Context) error {
					return os.MkdirAll(dir, 0o750)
				},
			})
		} else {
			probe.OK("сеансы консоли", "%s", cfg.Console.StatePath)
		}
	}
	if cfg.Console != nil && strings.HasPrefix(cfg.Console.PublicURL, "http://") {
		probe.Warn("канал до консоли", fmt.Sprintf("%s без шифрования", cfg.Console.PublicURL), diag.Remedy{
			Hint: "ссылка на вход и пароль видны любому по дороге; поставьте домен с сертификатом или выключите консоль",
		})
	}
}

func checkConsoleReach(ctx context.Context, opts Options, probe *diag.Probe) {
	base := strings.TrimRight(opts.Config.Console.PublicURL, "/")
	target := base + "/console/"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return
	}
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		probe.Warn("адрес консоли", fmt.Sprintf("%s недоступен: %v", target, err), diag.Remedy{
			Hint: "сервер не может достучаться до собственного публичного адреса; часто это нормально за NAT, но проверьте, что домен открывается снаружи",
		})
		return
	}
	defer response.Body.Close()
	if !opts.Running {
		probe.Skip("адрес консоли", "%s отвечает, но проверяемый сервер не запущен", base)
		return
	}
	if response.Header.Get("Upgrade") == "" && response.StatusCode == http.StatusBadGateway {
		probe.Fail("адрес консоли", fmt.Sprintf("%s отвечает %s", target, response.Status), diag.Remedy{
			Hint: "обратный прокси не доводит запрос до сервера — проверьте upstream в nginx",
		})
		return
	}
	probe.OK("адрес консоли", "%s отвечает (%s)", base, response.Status)
}

func checkLauncher(ctx context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.Launcher == nil || cfg.Launcher.Dir == "" {
		probe.Skip("лаунчер", "не задан launcher.dir — собирать лаунчеры на сервере нельзя")
		return
	}
	if err := writable(cfg.Launcher.Dir); err != nil {
		probe.Fail("папка лаунчеров", fmt.Sprintf("%s: %v", cfg.Launcher.Dir, err), diag.Remedy{
			Hint:    "каталог должен принадлежать пользователю сервера",
			Command: fmt.Sprintf("mkdir -p %s", cfg.Launcher.Dir),
			Apply: func(context.Context) error {
				return os.MkdirAll(cfg.Launcher.Dir, 0o750)
			},
		})
		return
	}
	probe.OK("папка лаунчеров", "%s", cfg.Launcher.Dir)

	if len(cfg.Launcher.Endpoints) == 0 {
		probe.Warn("адреса для лаунчера", "launcher.endpoints пуст", diag.Remedy{
			Hint:    "в собранный лаунчер попадёт адрес из api.addr — игрок с ним до сервера не достучится",
			Command: "laminara-server settings launcher.endpoints https://launcher.example.com",
		})
		return
	}
	for _, endpoint := range cfg.Launcher.Endpoints {
		checkEndpoint(ctx, probe, endpoint, opts)
	}
}

func checkEndpoint(ctx context.Context, probe *diag.Probe, endpoint string, opts Options) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		probe.Fail("адрес для лаунчера", fmt.Sprintf("%s — это не адрес", endpoint), diag.Remedy{
			Hint: "ожидается https://домен",
		})
		return
	}
	if parsed.Scheme == "http" {
		probe.Warn("адрес для лаунчера", fmt.Sprintf("%s без шифрования", endpoint), diag.Remedy{
			Hint: "пароли игроков пойдут открытым текстом; поставьте сертификат",
		})
	}
	target := strings.TrimRight(endpoint, "/") + "/healthz"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return
	}
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		probe.Warn("адрес для лаунчера", fmt.Sprintf("%s недоступен: %v", endpoint, err), diag.Remedy{
			Hint: "именно по этому адресу пойдут лаунчеры игроков; проверьте домен, сертификат и nginx",
		})
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		probe.Warn("адрес для лаунчера", fmt.Sprintf("%s отвечает %s", target, response.Status), diag.Remedy{
			Hint: "по этому адресу должен отвечать сервер Laminara; похоже, туда смотрит другой сайт",
		})
		return
	}
	if opts.Running {
		probe.OK("адрес для лаунчера", "%s отвечает", endpoint)
		return
	}
	probe.OK("адрес для лаунчера", "%s отвечает (сервер запущен отдельно)", endpoint)
}

func checkModules(_ context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.Modules == nil || cfg.Modules.Dir == "" {
		probe.Skip("модули", "каталог не задан — загружены только встроенные")
		return
	}
	entries, err := os.ReadDir(cfg.Modules.Dir)
	if err != nil {
		probe.Warn("папка модулей", fmt.Sprintf("%s: %v", cfg.Modules.Dir, err), diag.Remedy{
			Hint:    "создайте каталог или уберите modules.dir",
			Command: fmt.Sprintf("mkdir -p %s", cfg.Modules.Dir),
			Apply: func(context.Context) error {
				return os.MkdirAll(cfg.Modules.Dir, 0o750)
			},
		})
		return
	}
	runnable := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.Mode().Perm()&0o111 == 0 {
			path := filepath.Join(cfg.Modules.Dir, entry.Name())
			probe.Warn("модуль "+entry.Name(), "файл не исполняемый — сервер его пропустит", diag.Remedy{
				Hint:    "модуль загружается как отдельная программа, ему нужен флаг запуска",
				Command: fmt.Sprintf("chmod +x %s", path),
				Apply: func(context.Context) error {
					return os.Chmod(path, info.Mode().Perm()|0o755)
				},
			})
			continue
		}
		runnable++
	}
	if runnable == 0 && len(entries) == 0 {
		probe.Skip("модули", "%s пуст", cfg.Modules.Dir)
		return
	}
	probe.OK("модули", "готовы к загрузке: %d", runnable)
}

func checkRateLimit(_ context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.RateLimit != nil && cfg.RateLimit.Disabled {
		probe.Warn("защита от перебора", "выключена", diag.Remedy{
			Hint:    "пароли игроков можно будет перебирать без ограничений",
			Command: "laminara-server settings rateLimit.disabled false",
		})
		return
	}
	if cfg.RateLimit == nil {
		probe.OK("защита от перебора", "включена с настройками по умолчанию")
		return
	}
	if cfg.RateLimit.Backend == "redis" && !cfg.RateLimit.Redis.Set() {
		probe.Fail("защита от перебора", "выбран redis, но не задан rateLimit.redis.addr", diag.Remedy{
			Hint: "сервер не поднимется с такими настройками",
		})
		return
	}
	if cfg.RateLimit.Login.Limit > 0 && cfg.RateLimit.Login.Limit < 3 {
		probe.Warn("защита от перебора", fmt.Sprintf("на вход разрешено %d попыток", cfg.RateLimit.Login.Limit), diag.Remedy{
			Hint: "игрок, дважды опечатавшийся в пароле, окажется заблокирован",
		})
		return
	}
	probe.OK("защита от перебора", "включена, счётчики в %s", orElse(cfg.RateLimit.Backend, "памяти"))
}

func orElse(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func checkUpdate(ctx context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.Update != nil && !cfg.Update.Checks() {
		probe.Skip("обновления", "проверка выключена")
		return
	}
	probe.OK("текущая версия", "%s", version.Current)
	if cfg.Update != nil && cfg.Update.Install {
		checkReplaceable(probe)
	}
	repo := "laminara/laminara"
	if cfg.Update != nil {
		repo = cfg.Update.RepoOr(repo)
	}
	target := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return
	}
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		probe.Warn("источник обновлений", fmt.Sprintf("%s недоступен: %v", repo, err), diag.Remedy{
			Hint: "сервер не сможет узнать о новых версиях; проверьте выход в интернет",
		})
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		probe.Warn("источник обновлений", fmt.Sprintf("%s отвечает %s", repo, response.Status), diag.Remedy{
			Hint: "проверьте update.repo — репозиторий должен быть публичным",
		})
		return
	}
	probe.OK("источник обновлений", "%s отвечает", repo)
}

func checkReplaceable(probe *diag.Probe) {
	binary, err := os.Executable()
	if err != nil {
		return
	}
	dir := filepath.Dir(binary)
	if err := writable(dir); err != nil {
		probe.Fail("установка обновлений", fmt.Sprintf("%s недоступен на запись: %v", dir, err), diag.Remedy{
			Hint: "включён update.install, но заменить сам себя сервер не сможет; держите бинарь в каталоге данных и ссылайтесь на него симлинком",
		})
		return
	}
	probe.OK("установка обновлений", "включена, %s доступен на запись", dir)
}

func checkBranding(_ context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.Branding == nil {
		probe.Skip("оформление", "не задано — лаунчер соберётся со стандартным видом")
		return
	}
	if cfg.Branding.Name == "" {
		probe.Warn("название", "branding.name пуст", diag.Remedy{
			Hint:    "это имя видит игрок в окне лаунчера и в его свойствах",
			Command: "laminara-server settings branding.name \"Мой проект\"",
		})
	} else {
		probe.OK("название", "%s", cfg.Branding.Name)
	}
	checkAsset(probe, cfg.Branding.LogoPath, "логотип", "branding.logoPath")
	checkAsset(probe, cfg.Branding.HeroMediaPath, "фон", "branding.heroMediaPath")
}

func checkAsset(probe *diag.Probe, path, what, setting string) {
	if path == "" {
		probe.Skip(what, "не задан %s — используется стандартный", setting)
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		probe.Fail(what, fmt.Sprintf("%s: %v", path, err), diag.Remedy{
			Hint: "сборка лаунчера прервётся, пока файла нет",
		})
		return
	}
	if info.Size() == 0 {
		probe.Fail(what, fmt.Sprintf("%s пуст", path), diag.Remedy{
			Hint: "положите картинку на место или уберите настройку",
		})
		return
	}
	probe.OK(what, "%s, %s", path, humanize.Bytes(uint64(info.Size())))
}

func checkCrashes(_ context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.Crashes == nil || !cfg.Crashes.Enabled {
		probe.Skip("отчёты о падениях", "выключены — о вылетах игры вы узнаете только от игроков")
		return
	}
	if len(cfg.Crashes.Sinks) == 0 {
		probe.Warn("отчёты о падениях", "включены, но получателей нет", diag.Remedy{
			Hint: "отчёты будут собираться в никуда; добавьте получателя в crashReports.sinks",
		})
		return
	}
	known := map[string]bool{}
	for _, name := range crash.SinkNames() {
		known[name] = true
	}
	for name, sink := range cfg.Crashes.Sinks {
		if !known[sink.Type] {
			probe.Fail("получатель "+name, fmt.Sprintf("тип %q не зарегистрирован", sink.Type), diag.Remedy{
				Hint: fmt.Sprintf("доступны: %s", strings.Join(crash.SinkNames(), ", ")),
			})
			continue
		}
		probe.OK("получатель "+name, "%s", sink.Type)
	}
}
