package remote

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"

	sdkmodule "github.com/laminara/laminara/sdk/go/module"
	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/events"
	"github.com/laminara/laminara/server/internal/module"
)

const (
	moduleLoadTimeout    = 30 * time.Second
	eventDispatchTimeout = 10 * time.Second
	eventQueueSize       = 32
)

type eventTarget struct {
	name   string
	topics map[string]bool
	svc    sdkmodule.Service
	queue  chan events.Event
}

type Loader struct {
	clients   []*goplugin.Client
	handlers  []*eventTarget
	log       *slog.Logger
	done      chan struct{}
	closeOnce sync.Once
}

func NewLoader(log *slog.Logger) *Loader {
	return &Loader{log: log, done: make(chan struct{})}
}

func (l *Loader) LoadDir(dir string, configs map[string][]byte, registry *module.Registry) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if !executable(path) {
			continue
		}
		if err := l.load(path, configs[entry.Name()], registry); err != nil {
			l.log.Error("module load failed", "path", path, "error", err)
		}
	}
	return nil
}

func (l *Loader) load(path string, config []byte, registry *module.Registry) error {
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  sdkmodule.Handshake,
		Plugins:          sdkmodule.PluginSet(nil),
		Cmd:              exec.Command(path),
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Logger:           hclog.NewNullLogger(),
	})
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return err
	}
	raw, err := rpcClient.Dispense(sdkmodule.PluginName)
	if err != nil {
		client.Kill()
		return err
	}
	svc, ok := raw.(sdkmodule.Service)
	if !ok {
		client.Kill()
		return fmt.Errorf("unexpected plugin type %T", raw)
	}

	ctx, cancel := context.WithTimeout(context.Background(), moduleLoadTimeout)
	defer cancel()
	if len(config) == 0 {
		config = []byte("{}")
	}
	if err := svc.Configure(ctx, config); err != nil {
		client.Kill()
		return err
	}
	manifest, err := svc.Info(ctx)
	if err != nil {
		client.Kill()
		return err
	}
	registry.Add(newRemoteModule(manifest.Info, manifest.Commands, svc))
	l.clients = append(l.clients, client)
	if len(manifest.Events) > 0 {
		set := make(map[string]bool, len(manifest.Events))
		for _, topic := range manifest.Events {
			set[topic] = true
		}
		l.handlers = append(l.handlers, &eventTarget{
			name:   manifest.Info.Name,
			topics: set,
			svc:    svc,
			queue:  make(chan events.Event, eventQueueSize),
		})
	}
	l.registerProviders(manifest.Info.Name, manifest.Providers, svc)
	l.log.Info("module loaded",
		"name", manifest.Info.Name,
		"version", manifest.Info.Version,
		"commands", len(manifest.Commands),
		"events", len(manifest.Events),
		"providers", len(manifest.Providers),
	)
	return nil
}

func (l *Loader) Subscribe(bus *events.Bus) {
	if bus == nil || len(l.handlers) == 0 {
		return
	}
	for _, target := range l.handlers {
		go l.worker(target)
	}
	bus.Subscribe(func(e events.Event) {
		for _, target := range l.handlers {
			if !target.topics[e.Topic] {
				continue
			}
			select {
			case target.queue <- e:
			case <-l.done:
			default:
				l.log.Warn("module event queue full, dropping", "module", target.name, "topic", e.Topic)
			}
		}
	})
}

func (l *Loader) worker(target *eventTarget) {
	for {
		select {
		case e := <-target.queue:
			l.dispatch(target, e)
		case <-l.done:
			return
		}
	}
}

func (l *Loader) dispatch(target *eventTarget, e events.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), eventDispatchTimeout)
	defer cancel()
	if err := target.svc.Emit(ctx, e.Topic, e.Data); err != nil {
		l.log.Error("module event dispatch failed", "module", target.name, "topic", e.Topic, "error", err)
		return
	}
	l.log.Info("module event dispatched", "module", target.name, "topic", e.Topic)
}

func (l *Loader) Close() {
	l.closeOnce.Do(func() { close(l.done) })
	for _, client := range l.clients {
		client.Kill()
	}
	l.clients = nil
}

type remoteModule struct {
	info  module.Info
	specs []sdkmodule.CommandSpec
	svc   sdkmodule.Service
}

func newRemoteModule(info sdkmodule.Info, specs []sdkmodule.CommandSpec, svc sdkmodule.Service) *remoteModule {
	return &remoteModule{
		info:  module.Info{Name: info.Name, Version: info.Version, Description: info.Description},
		specs: specs,
		svc:   svc,
	}
}

func (r *remoteModule) Info() module.Info { return r.info }

func (r *remoteModule) Register(host module.Host) error {
	for _, spec := range r.specs {
		name := spec.Name
		host.AddCommand(command.Command{
			Name:     name,
			Aliases:  spec.Aliases,
			Synopsis: spec.Synopsis,
			Run: func(ctx context.Context, args []string, out io.Writer) error {
				return r.svc.Execute(ctx, name, args, out)
			},
		})
	}
	return nil
}

func executable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}
