package daemon

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	sddaemon "github.com/coreos/go-systemd/v22/daemon"

	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
	"github.com/laminara/laminara/server/internal/access"
	"github.com/laminara/laminara/server/internal/admin"
	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/buildsvc"
	"github.com/laminara/laminara/server/internal/catalog"
	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/control"
	"github.com/laminara/laminara/server/internal/events"
	"github.com/laminara/laminara/server/internal/hwid"
	"github.com/laminara/laminara/server/internal/launchersvc"
	"github.com/laminara/laminara/server/internal/logbus"
	"github.com/laminara/laminara/server/internal/module"
	"github.com/laminara/laminara/server/internal/module/builtin"
	"github.com/laminara/laminara/server/internal/module/remote"
	"github.com/laminara/laminara/server/internal/signing"
	"github.com/laminara/laminara/server/internal/version"
)

const (
	shutdownTimeout = 10 * time.Second
	maxCommandLine  = 64 << 10
)

type Daemon struct {
	startedAt     time.Time
	level         *slog.LevelVar
	bus           *logbus.Bus
	registry      *command.Registry
	modules       *module.Registry
	moduleLoader  *remote.Loader
	log           *slog.Logger
	publicHandler http.Handler
	publicAddr    string
	catalog       admin.Catalog
}

type Options struct {
	Auth          *auth.Service
	Access        *access.Controller
	Catalog       *catalog.Catalog
	Machines      *hwid.Gate
	Signing       *signing.Keyring
	Build         *buildsvc.Service
	Launcher      *launchersvc.Service
	PublicHandler http.Handler
	PublicAddr    string
	ModulesDir    string
	ModulesConfig map[string][]byte
	Events        *events.Bus
}

func New(opts Options) *Daemon {
	bus := logbus.NewBus(4096)
	level := new(slog.LevelVar)
	level.Set(slog.LevelInfo)
	log := slog.New(logbus.NewHandler(os.Stdout, level, bus))
	slog.SetDefault(log)

	registry := command.NewRegistry()
	registry.Register(command.HelpCommand(registry))

	d := &Daemon{
		startedAt:     time.Now(),
		level:         level,
		bus:           bus,
		registry:      registry,
		log:           log,
		publicHandler: opts.PublicHandler,
		publicAddr:    opts.PublicAddr,
	}
	registry.Register(d.statusCommand())
	registry.Register(d.versionCommand())
	if opts.Auth != nil {
		registry.Register(authCommand(opts.Auth))
	}
	if opts.Catalog != nil {
		registry.Register(accessCommand(opts.Access, opts.Catalog.List))
	}
	registry.Register(hwidCommand(opts.Machines))
	if opts.Signing != nil {
		registry.Register(signingCommand(opts.Signing))
	}
	if opts.Machines != nil {
		registry.Register(machinesCommand(opts.Machines))
		registry.Register(banCommand(opts.Machines))
		registry.Register(bansCommand(opts.Machines))
	}
	if opts.Launcher != nil {
		for _, launcherCommand := range opts.Launcher.Commands() {
			registry.Register(launcherCommand)
		}
	}
	if opts.Build != nil {
		d.catalog = opts.Build
		for _, buildCommand := range opts.Build.Commands() {
			registry.Register(buildCommand)
		}
	}

	modules := module.NewRegistry()
	modules.Add(builtin.NewDiagnostics(modules))
	if opts.ModulesDir != "" {
		d.moduleLoader = remote.NewLoader(log)
		if err := d.moduleLoader.LoadDir(opts.ModulesDir, opts.ModulesConfig, modules); err != nil {
			log.Error("modules dir scan failed", "dir", opts.ModulesDir, "error", err)
		}
		d.moduleLoader.Subscribe(opts.Events)
	}
	if err := modules.Load(module.CommandHost{Registry: registry}); err != nil {
		log.Error("module load failed", "error", err)
	}
	d.modules = modules
	return d
}

func (d *Daemon) status() admin.Status {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	modulesLoaded := uint32(0)
	if d.modules != nil {
		modulesLoaded = uint32(len(d.modules.Loaded()))
	}
	return admin.Status{
		Version:       version.Current,
		StartedAtNano: d.startedAt.UnixNano(),
		ModulesLoaded: modulesLoaded,
		MemoryBytes:   mem.Alloc,
		Goroutines:    uint32(runtime.NumGoroutine()),
	}
}

func (d *Daemon) Run(ctx context.Context) error {
	if d.moduleLoader != nil {
		defer d.moduleLoader.Close()
	}
	var publicListener net.Listener
	if d.publicHandler != nil && d.publicAddr != "" {
		opened, err := net.Listen("tcp", d.publicAddr)
		if err != nil {
			return err
		}
		publicListener = opened
		defer publicListener.Close()
	}

	listener, err := control.Listen()
	if err != nil {
		return err
	}
	defer listener.Close()
	defer os.Remove(control.SocketPath())

	if err := os.WriteFile(control.PidPath(), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return err
	}
	defer os.Remove(control.PidPath())

	service := admin.NewService(d.status, d.bus, d.registry, d.catalog)
	mux := http.NewServeMux()
	mux.Handle(adminv1connect.NewAdminServiceHandler(service))
	server := &http.Server{Handler: mux}

	serveErr := make(chan error, 2)
	go func() { serveErr <- server.Serve(listener) }()

	var publicServer *http.Server
	if publicListener != nil {
		publicServer = &http.Server{Handler: d.publicHandler}
		go func() { serveErr <- publicServer.Serve(publicListener) }()
	}

	go d.readCommands(ctx)

	d.log.Info("laminara-server started",
		"source", "daemon",
		"version", version.Current,
		"socket", control.SocketPath(),
		"api", d.publicAddr,
	)
	_, _ = sddaemon.SdNotify(false, sddaemon.SdNotifyReady)

	select {
	case <-ctx.Done():
		d.log.Info("shutting down", "source", "daemon")
		_, _ = sddaemon.SdNotify(false, sddaemon.SdNotifyStopping)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if publicServer != nil {
			_ = publicServer.Shutdown(shutdownCtx)
		}
		return server.Shutdown(shutdownCtx)
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (d *Daemon) readCommands(ctx context.Context) {
	d.dispatchLines(ctx, os.Stdin)
}

func (d *Daemon) dispatchLines(ctx context.Context, input io.Reader) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 0, 4096), maxCommandLine)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		var out bytes.Buffer
		if err := d.registry.Dispatch(ctx, line, &out); err != nil {
			d.log.Error("command failed", "source", "console", "command", line, "error", err)
			continue
		}
		reply := strings.TrimRight(out.String(), "\n")
		if reply == "" {
			continue
		}
		for _, line := range strings.Split(reply, "\n") {
			d.log.Info(line, "source", "console")
		}
	}
}

func (d *Daemon) statusCommand() command.Command {
	return command.Command{
		Name:     "status",
		Synopsis: "show server status",
		Run: func(_ context.Context, _ []string, out io.Writer) error {
			s := d.status()
			uptime := time.Since(time.Unix(0, s.StartedAtNano)).Truncate(time.Second)
			fmt.Fprintf(out, "version:    %s\n", s.Version)
			fmt.Fprintf(out, "uptime:     %s\n", uptime)
			fmt.Fprintf(out, "modules:    %d\n", s.ModulesLoaded)
			fmt.Fprintf(out, "memory:     %d KiB\n", s.MemoryBytes/1024)
			fmt.Fprintf(out, "goroutines: %d\n", s.Goroutines)
			return nil
		},
	}
}

func (d *Daemon) versionCommand() command.Command {
	return command.Command{
		Name:     "version",
		Synopsis: "show server version",
		Run: func(_ context.Context, _ []string, out io.Writer) error {
			fmt.Fprintln(out, version.Current)
			return nil
		},
	}
}

func authCommand(service *auth.Service) command.Command {
	return command.Command{
		Name:     "auth",
		Synopsis: "exercise the auth provider (auth test <user> <pass> | auth validate <token>)",
		Run: func(ctx context.Context, args []string, out io.Writer) error {
			if len(args) == 0 {
				return errors.New("usage: auth test <username> <password> | auth validate <token>")
			}
			switch args[0] {
			case "test":
				if len(args) < 3 {
					return errors.New("usage: auth test <username> <password>")
				}
				tokens, err := service.Login(ctx, args[1], args[2])
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "ok\naccess:  %s\nrefresh: %s\n", tokens.Access, tokens.Refresh)
				return nil
			case "validate":
				if len(args) < 2 {
					return errors.New("usage: auth validate <accessToken>")
				}
				identity, err := service.ValidateAccess(ctx, args[1])
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "username: %s\nuuid:     %s\n", identity.Username, identity.UUID)
				return nil
			default:
				return fmt.Errorf("unknown auth subcommand %q", args[0])
			}
		},
	}
}
