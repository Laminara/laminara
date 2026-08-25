package buildsvc

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/laminara/laminara/server/internal/admin"
	"github.com/laminara/laminara/server/internal/buildview"
	"github.com/laminara/laminara/server/internal/slp"
)

const pingTimeout = 2 * time.Second

func (s *Service) Players(_ context.Context) ([]admin.BuildPlayers, error) {
	builds, err := s.Builds()
	if err != nil {
		return nil, err
	}
	list := make([]admin.BuildPlayers, len(builds))
	var wait sync.WaitGroup
	for i, build := range builds {
		list[i] = admin.BuildPlayers{Build: build.Name, Address: build.ServerAddress}
		if build.ServerAddress == "" {
			continue
		}
		wait.Add(1)
		go func(at int, address string) {
			defer wait.Done()
			status, err := slp.Ping(address, pingTimeout)
			if err != nil {
				list[at].Error = err.Error()
				return
			}
			list[at].Reachable = true
			list[at].Online = status.Online
			list[at].Max = status.Max
			list[at].Names = status.Sample
			list[at].Version = status.Version
		}(i, build.ServerAddress)
	}
	wait.Wait()
	sort.Slice(list, func(i, j int) bool { return list[i].Build < list[j].Build })
	return list, nil
}

func (s *Service) players(ctx context.Context, _ []string, out io.Writer) error {
	list, err := s.Players(ctx)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintln(out, "Сборок пока нет — опрашивать некого.")
		return nil
	}
	width := 0
	for _, entry := range list {
		width = max(width, len([]rune(entry.Build)))
	}
	total := int64(0)
	asked := 0
	missingAddress := false
	for _, entry := range list {
		name := entry.Build + strings.Repeat(" ", width-len([]rune(entry.Build)))
		switch {
		case entry.Address == "":
			missingAddress = true
			fmt.Fprintf(out, "%s  адрес сервера не указан\n", name)
		case !entry.Reachable:
			fmt.Fprintf(out, "%s  сервер не отвечает (%s)\n", name, entry.Address)
			asked++
		default:
			asked++
			total += entry.Online
			line := fmt.Sprintf("%s  %d из %d", name, entry.Online, entry.Max)
			if len(entry.Names) > 0 {
				line += "   " + strings.Join(entry.Names, ", ")
			}
			fmt.Fprintln(out, line)
		}
	}
	if asked > 1 {
		fmt.Fprintf(out, "Всего в игре: %d\n", total)
	}
	if missingAddress {
		fmt.Fprintln(out, buildview.AddressNote)
	}
	return nil
}

func (s *Service) buildInfo(ctx context.Context, args []string, out io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("напишите имя сборки: build <имя>")
	}
	name := args[0]
	builds, err := s.Builds()
	if err != nil {
		return err
	}
	var found *admin.BuildEntry
	for i := range builds {
		if strings.EqualFold(builds[i].Name, name) {
			found = &builds[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("сборки «%s» нет — список даёт команда builds", name)
	}
	var players *admin.BuildPlayers
	if found.ServerAddress != "" {
		list, err := s.Players(ctx)
		if err == nil {
			for i := range list {
				if list[i].Build == found.Name {
					players = &list[i]
					break
				}
			}
		}
	}
	info := admin.BuildInfoOf(*found)
	fmt.Fprintln(out, buildview.Title(info))
	for _, field := range buildview.Fields(info, admin.PlayersInfoOf(players)) {
		line := fmt.Sprintf("  %-16s %s", field.Label, field.Value)
		if field.Hint != "" {
			line += "   " + field.Hint
		}
		fmt.Fprintln(out, line)
	}
	return nil
}
