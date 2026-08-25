package daemon

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/selfupdate"
	"github.com/laminara/laminara/server/internal/version"
)

const firstCheckDelay = time.Minute

func (d *Daemon) checker() *selfupdate.Checker {
	return &selfupdate.Checker{Repo: d.update.RepoOr(selfupdate.DefaultRepo)}
}

func (d *Daemon) updateCommand() command.Command {
	return command.Command{
		Name:     "update",
		Synopsis: "обновить сервер до свежего релиза (update check — только посмотреть)",
		Run: func(ctx context.Context, args []string, out io.Writer) error {
			if len(args) > 1 || (len(args) == 1 && args[0] != "check") {
				return fmt.Errorf("update | update check")
			}
			return selfupdate.Run(ctx, d.checker(), version.Current, len(args) == 1, "restart", out)
		},
	}
}

func (d *Daemon) watchUpdates(ctx context.Context) {
	if !d.update.Checks() {
		return
	}
	interval := d.update.IntervalOr(selfupdate.DefaultInterval)
	timer := time.NewTimer(firstCheckDelay)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.quit:
			return
		case <-timer.C:
			d.lookForUpdate(ctx)
			timer.Reset(interval)
		}
	}
}

func (d *Daemon) lookForUpdate(ctx context.Context) {
	checker := d.checker()
	release, err := checker.Latest(ctx)
	if err != nil {
		d.log.Debug("проверить обновление не удалось", "source", "update", "ошибка", err)
		return
	}
	if !release.IsNewerThan(version.Current) {
		return
	}

	if !d.update.Installs() {
		d.log.Info("вышла новая версия сервера",
			"source", "update",
			"версия", release.Version,
			"установлена", version.Current,
			"поставить", "laminara-server update",
		)
		return
	}

	if err := checker.Apply(ctx, release); err != nil {
		d.log.Error("обновление не установлено", "source", "update", "версия", release.Version, "ошибка", err)
		return
	}
	d.log.Info("обновление установлено, перезапускаюсь", "source", "update", "версия", release.Version)
	if err := d.RequestRestart(); err != nil {
		d.log.Error("перезапуск не удался", "source", "update", "ошибка", err)
	}
}
