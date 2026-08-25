package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/humanize"
	"github.com/laminara/laminara/server/internal/hwid"
)

func machinesCommand(gate *hwid.Gate) command.Command {
	return command.Command{
		Name:     "machines",
		Synopsis: "компьютеры игроков (machines <игрок> | machines show <id> | machines trust <id> [off] | machines prune)",
		Run: func(ctx context.Context, args []string, out io.Writer) error {
			if !gate.Enabled() {
				return errors.New("machine recognition is off (set hwid.mode)")
			}
			if len(args) == 0 {
				return errors.New("usage: machines <player> | machines show <machineId> | machines trust <machineId> [off] | machines prune")
			}
			switch args[0] {
			case "show":
				if len(args) < 2 {
					return errors.New("usage: machines show <machineId>")
				}
				return showMachine(ctx, gate, args[1], out)
			case "trust":
				if len(args) < 2 {
					return errors.New("usage: machines trust <machineId> [off]")
				}
				trusted := len(args) < 3 || !strings.EqualFold(args[2], "off")
				if err := gate.Store().SetTrusted(ctx, args[1], trusted); err != nil {
					return err
				}
				fmt.Fprintf(out, "Компьютер %s: доверенный — %s\n", args[1], yesNo(trusted))
				return nil
			case "prune":
				removed, err := gate.Prune(ctx)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Забыто неактивных компьютеров: %d\n", removed)
				return nil
			default:
				return listPlayerMachines(ctx, gate, args[0], out)
			}
		},
	}
}

func listPlayerMachines(ctx context.Context, gate *hwid.Gate, player string, out io.Writer) error {
	subject, err := gate.SubjectOf(ctx, player)
	if err != nil {
		return err
	}
	machines, err := gate.Store().MachinesOfSubject(ctx, subject)
	if err != nil {
		return err
	}
	if len(machines) == 0 {
		fmt.Fprintf(out, "У игрока %s ещё не было ни одного входа\n", player)
		return nil
	}
	for _, machine := range machines {
		fmt.Fprintf(out, "%s  кластер %s  последний вход %s%s\n",
			machine.ID, short(machine.ClusterID), machine.LastSeen.Format(time.RFC3339), trustedSuffix(machine.Trusted))
		if len(machine.Flags) > 0 {
			fmt.Fprintf(out, "  особенности: %s\n", flagWords(machine.Flags))
		}
	}
	return nil
}

func showMachine(ctx context.Context, gate *hwid.Gate, machineID string, out io.Writer) error {
	machine, err := gate.Store().Machine(ctx, machineID)
	if err != nil {
		return err
	}
	if machine == nil {
		return fmt.Errorf("компьютера %s нет в списке", machineID)
	}
	fmt.Fprintf(out, "компьютер: %s%s\n", machine.ID, trustedSuffix(machine.Trusted))
	fmt.Fprintf(out, "кластер:   %s\n", machine.ClusterID)
	fmt.Fprintf(out, "платформа: %s\n", machine.Platform)
	fmt.Fprintf(out, "виден:     с %s по %s\n", machine.FirstSeen.Format(time.RFC3339), machine.LastSeen.Format(time.RFC3339))
	if len(machine.Flags) > 0 {
		fmt.Fprintf(out, "особенности: %s\n", flagWords(machine.Flags))
	}

	accounts, err := gate.Store().AccountsOfMachine(ctx, machine.ID)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "аккаунты:")
	for _, account := range accounts {
		fmt.Fprintf(out, "  %-20s %s\n", account.Username, account.LastSeen.Format(time.RFC3339))
	}

	siblings, err := gate.Store().MachinesOfCluster(ctx, machine.ClusterID)
	if err != nil {
		return err
	}
	if len(siblings) > 1 {
		fmt.Fprintf(out, "в кластере компьютеров: %d\n", len(siblings))
	}
	return nil
}

func banCommand(gate *hwid.Gate) command.Command {
	return command.Command{
		Name:     "ban",
		Synopsis: "забанить игрока (ban <игрок> [причина…] [--account|--machine <id>|--cluster <id>] [--days N|--permanent] [--yes])",
		Run: func(ctx context.Context, args []string, out io.Writer) error {
			if !gate.Enabled() {
				return errors.New("machine recognition is off (set hwid.mode)")
			}
			if len(args) == 0 {
				return errors.New("usage: ban <player> [reason...] [--account|--machine <id>|--cluster <id>] [--days N|--permanent] [--yes]")
			}
			request, reason, err := parseBanArgs(args[1:])
			if err != nil {
				return err
			}
			subject, err := gate.SubjectOf(ctx, args[0])
			if err != nil {
				return err
			}
			request.Username = args[0]
			request.Subject = subject
			request.Reason = reason
			request.By = "console"

			outcome, err := gate.Ban(ctx, request)
			if errors.Is(err, hwid.ErrNothingToBan) && request.Scope == apiv1.BanScope_BAN_SCOPE_UNSPECIFIED {
				return errors.New("no machine has been seen for this player; use --account to ban the account")
			}
			if err != nil {
				return err
			}
			if outcome.NeedsConfirmation {
				fmt.Fprintf(out, "Такой бан (%s) заденет ещё %d аккаунтов:\n", hwid.ScopeWord(outcome.Scope), len(outcome.Accounts))
				for _, account := range outcome.Accounts {
					fmt.Fprintf(out, "  %s\n", account.Username)
				}
				fmt.Fprintln(out, "Повторите с --yes, чтобы применить, или с --account, чтобы забанить только этого игрока.")
				return nil
			}
			fmt.Fprintf(out, "Забанен %s (%s), код обращения %s\n", outcome.Ban.Target, hwid.ScopeWord(outcome.Scope), outcome.Ban.Reference)
			if outcome.Ban.ExpiresAt.IsZero() {
				fmt.Fprintln(out, "истекает: никогда")
			} else {
				fmt.Fprintf(out, "истекает: %s\n", outcome.Ban.ExpiresAt.Format(time.RFC3339))
			}
			if outcome.Machines > 1 {
				fmt.Fprintf(out, "под баном компьютеров: %d, аккаунтов: %d\n", outcome.Machines, len(outcome.Accounts))
			}
			if gate.Mode() != hwid.ModeEnforce {
				fmt.Fprintf(out, "Внимание: распознавание компьютеров в режиме %q — бан записан, но не применяется\n", gate.Mode())
			}
			return nil
		},
	}
}

func parseBanArgs(args []string) (hwid.BanRequest, string, error) {
	request := hwid.BanRequest{}
	var reason []string
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--account":
			request.Scope = apiv1.BanScope_BAN_SCOPE_ACCOUNT
		case "--machine":
			if index+1 >= len(args) {
				return request, "", errors.New("--machine needs a machine id")
			}
			index++
			request.MachineID = args[index]
		case "--cluster":
			if index+1 >= len(args) {
				return request, "", errors.New("--cluster needs a cluster id")
			}
			index++
			request.ClusterID = args[index]
		case "--days":
			if index+1 >= len(args) {
				return request, "", errors.New("--days needs a number")
			}
			index++
			days, err := strconv.Atoi(args[index])
			if err != nil || days <= 0 {
				return request, "", fmt.Errorf("--days ждёт положительное число, а получил %q", args[index])
			}
			request.TTL = time.Duration(days) * 24 * time.Hour
		case "--permanent":
			request.Permanent = true
		case "--yes":
			request.Confirm = true
		default:
			reason = append(reason, args[index])
		}
	}
	return request, strings.Join(reason, " "), nil
}

func bansCommand(gate *hwid.Gate) command.Command {
	return command.Command{
		Name:     "bans",
		Synopsis: "баны (bans [--all] | bans <код> | bans lift <код>)",
		Run: func(ctx context.Context, args []string, out io.Writer) error {
			if !gate.Enabled() {
				return errors.New("machine recognition is off (set hwid.mode)")
			}
			switch {
			case len(args) == 0 || args[0] == "--all":
				bans, err := gate.Bans(ctx, len(args) > 0)
				if err != nil {
					return err
				}
				if len(bans) == 0 {
					fmt.Fprintln(out, "Банов нет.")
					return nil
				}
				for _, ban := range bans {
					fmt.Fprintf(out, "%s  %-8s %-38s %s\n", ban.Reference, hwid.ScopeWord(ban.Scope), short(ban.Target), ban.Reason)
				}
				return nil
			case args[0] == "lift":
				if len(args) < 2 {
					return errors.New("usage: bans lift <reference>")
				}
				if err := gate.Unban(ctx, strings.ToUpper(args[1])); err != nil {
					return err
				}
				fmt.Fprintf(out, "Бан %s снят\n", strings.ToUpper(args[1]))
				return nil
			default:
				return showBan(ctx, gate, strings.ToUpper(args[0]), out)
			}
		},
	}
}

func showBan(ctx context.Context, gate *hwid.Gate, reference string, out io.Writer) error {
	ban, err := gate.BanByReference(ctx, reference)
	if err != nil {
		return err
	}
	if ban == nil {
		return fmt.Errorf("бана с кодом %s нет", reference)
	}
	fmt.Fprintf(out, "код:       %s\n", ban.Reference)
	fmt.Fprintf(out, "область:   %s\n", hwid.ScopeWord(ban.Scope))
	fmt.Fprintf(out, "кого:      %s\n", ban.Target)
	fmt.Fprintf(out, "причина:   %s\n", ban.Reason)
	fmt.Fprintf(out, "выдал:     %s\n", ban.By)
	fmt.Fprintf(out, "выдан:     %s\n", ban.CreatedAt.Format(time.RFC3339))
	if ban.ExpiresAt.IsZero() {
		fmt.Fprintln(out, "истекает:  никогда")
	} else {
		fmt.Fprintf(out, "истекает:  %s\n", ban.ExpiresAt.Format(time.RFC3339))
	}
	fmt.Fprintf(out, "действует: %s\n", yesNo(ban.Active(time.Now())))
	return nil
}

func hwidCommand(gate *hwid.Gate) command.Command {
	return command.Command{
		Name:     "hwid",
		Synopsis: "как настроено распознавание компьютеров",
		Run: func(_ context.Context, _ []string, out io.Writer) error {
			if !gate.Enabled() {
				fmt.Fprintln(out, "распознавание компьютеров выключено")
				return nil
			}
			cfg := gate.Config()
			fmt.Fprintf(out, "режим:            %s\n", modeWord(cfg.Mode))
			fmt.Fprintf(out, "хранилище:        %s\n", cfg.Store.Backend)
			fmt.Fprintf(out, "совпадение:       вес от %d и не меньше %d признаков; кластер от %d\n", cfg.MinScore, cfg.MinKinds, cfg.ClusterScore)
			fmt.Fprintf(out, "обесценивание:    признак встречается больше чем на %d компьютерах\n", cfg.FanOutLimit)
			fmt.Fprintf(out, "виртуальные машины: %s\n", vmWord(cfg.VMPolicy))
			fmt.Fprintf(out, "бан по железу:    истекает через %s\n", humanize.Duration(cfg.HardwareBanTTL.Duration()))
			fmt.Fprintf(out, "требовать отчёт:  %s\n", yesNo(cfg.RequireReport))
			fmt.Fprintf(out, "вход в игру только через лаунчер: %s\n", yesNo(cfg.RequireLauncher))
			fmt.Fprintf(out, "требовать аппаратный ключ:        %s\n", yesNo(cfg.RequireHardwareKey))
			return nil
		},
	}
}

func trustedSuffix(trusted bool) string {
	if trusted {
		return "  [trusted]"
	}
	return ""
}

func short(value string) string {
	if len(value) > 36 {
		return value[:36]
	}
	return value
}

func flagWords(flags []string) string {
	words := make([]string, 0, len(flags))
	for _, flag := range flags {
		words = append(words, flagWord(flag))
	}
	return strings.Join(words, ", ")
}

func flagWord(flag string) string {
	switch flag {
	case "COLLECTOR_FLAG_VIRTUAL_MACHINE":
		return "виртуальная машина"
	case "COLLECTOR_FLAG_ELEVATED":
		return "запуск с правами администратора"
	case "COLLECTOR_FLAG_PLATFORM_KEY_FALLBACK":
		return "ключ хранится без TPM"
	case "COLLECTOR_FLAG_SMBIOS_UNREADABLE":
		return "не читается SMBIOS"
	case "COLLECTOR_FLAG_CONTAINER":
		return "запуск в контейнере"
	case "COLLECTOR_FLAG_WEAK":
		return "мало признаков — узнать машину трудно"
	default:
		return flag
	}
}

func modeWord(mode hwid.Mode) string {
	switch mode {
	case "enforce":
		return "enforce — машины узнаются, баны применяются"
	case "observe":
		return "observe — машины узнаются, но баны не применяются"
	case "off":
		return "off — распознавание выключено"
	default:
		return string(mode)
	}
}

func vmWord(policy hwid.VMPolicy) string {
	switch policy {
	case "flag":
		return "flag — отмечать, но пускать"
	case "block":
		return "block — не пускать"
	case "allow":
		return "allow — считать обычной машиной"
	default:
		return string(policy)
	}
}

func yesNo(value bool) string {
	if value {
		return "да"
	}
	return "нет"
}
