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
	"github.com/laminara/laminara/server/internal/hwid"
)

func machinesCommand(gate *hwid.Gate) command.Command {
	return command.Command{
		Name:     "machines",
		Synopsis: "inspect known machines (machines <player> | machines show <machineId> | machines trust <machineId> [off] | machines prune)",
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
				fmt.Fprintf(out, "machine %s trusted: %t\n", args[1], trusted)
				return nil
			case "prune":
				removed, err := gate.Prune(ctx)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "pruned %d idle machines\n", removed)
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
		fmt.Fprintf(out, "no machine seen for %s\n", player)
		return nil
	}
	for _, machine := range machines {
		fmt.Fprintf(out, "%s  cluster %s  last seen %s%s\n",
			machine.ID, short(machine.ClusterID), machine.LastSeen.Format(time.RFC3339), trustedSuffix(machine.Trusted))
		if len(machine.Flags) > 0 {
			fmt.Fprintf(out, "  flags: %s\n", strings.Join(machine.Flags, ", "))
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
		return fmt.Errorf("no machine %s", machineID)
	}
	fmt.Fprintf(out, "machine:  %s%s\n", machine.ID, trustedSuffix(machine.Trusted))
	fmt.Fprintf(out, "cluster:  %s\n", machine.ClusterID)
	fmt.Fprintf(out, "platform: %s\n", machine.Platform)
	fmt.Fprintf(out, "seen:     %s .. %s\n", machine.FirstSeen.Format(time.RFC3339), machine.LastSeen.Format(time.RFC3339))
	if len(machine.Flags) > 0 {
		fmt.Fprintf(out, "flags:    %s\n", strings.Join(machine.Flags, ", "))
	}

	accounts, err := gate.Store().AccountsOfMachine(ctx, machine.ID)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "accounts:")
	for _, account := range accounts {
		fmt.Fprintf(out, "  %-20s %s\n", account.Username, account.LastSeen.Format(time.RFC3339))
	}

	siblings, err := gate.Store().MachinesOfCluster(ctx, machine.ClusterID)
	if err != nil {
		return err
	}
	if len(siblings) > 1 {
		fmt.Fprintf(out, "cluster holds %d machines\n", len(siblings))
	}
	return nil
}

func banCommand(gate *hwid.Gate) command.Command {
	return command.Command{
		Name:     "ban",
		Synopsis: "ban a player (ban <player> [reason...] [--account|--machine <id>|--cluster <id>] [--days N|--permanent] [--yes])",
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
				fmt.Fprintf(out, "this %s ban would also stop %d accounts:\n", hwid.ScopeWord(outcome.Scope), len(outcome.Accounts))
				for _, account := range outcome.Accounts {
					fmt.Fprintf(out, "  %s\n", account.Username)
				}
				fmt.Fprintln(out, "re-run with --yes to apply it, or use --account to ban only this player")
				return nil
			}
			fmt.Fprintf(out, "banned %s [%s] reference %s\n", outcome.Ban.Target, hwid.ScopeWord(outcome.Scope), outcome.Ban.Reference)
			if outcome.Ban.ExpiresAt.IsZero() {
				fmt.Fprintln(out, "expires: never")
			} else {
				fmt.Fprintf(out, "expires: %s\n", outcome.Ban.ExpiresAt.Format(time.RFC3339))
			}
			if outcome.Machines > 1 {
				fmt.Fprintf(out, "covers %d machines and %d accounts\n", outcome.Machines, len(outcome.Accounts))
			}
			if gate.Mode() != hwid.ModeEnforce {
				fmt.Fprintf(out, "note: hwid.mode is %q, so this ban is recorded but not enforced\n", gate.Mode())
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
				return request, "", fmt.Errorf("--days wants a positive number, got %q", args[index])
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
		Synopsis: "list bans (bans [--all] | bans <reference> | bans lift <reference>)",
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
					fmt.Fprintln(out, "no bans")
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
				fmt.Fprintf(out, "lifted %s\n", strings.ToUpper(args[1]))
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
		return fmt.Errorf("no ban with reference %s", reference)
	}
	fmt.Fprintf(out, "reference: %s\n", ban.Reference)
	fmt.Fprintf(out, "scope:     %s\n", hwid.ScopeWord(ban.Scope))
	fmt.Fprintf(out, "target:    %s\n", ban.Target)
	fmt.Fprintf(out, "reason:    %s\n", ban.Reason)
	fmt.Fprintf(out, "by:        %s\n", ban.By)
	fmt.Fprintf(out, "created:   %s\n", ban.CreatedAt.Format(time.RFC3339))
	if ban.ExpiresAt.IsZero() {
		fmt.Fprintln(out, "expires:   never")
	} else {
		fmt.Fprintf(out, "expires:   %s\n", ban.ExpiresAt.Format(time.RFC3339))
	}
	fmt.Fprintf(out, "active:    %t\n", ban.Active(time.Now()))
	return nil
}

func hwidCommand(gate *hwid.Gate) command.Command {
	return command.Command{
		Name:     "hwid",
		Synopsis: "show machine recognition settings",
		Run: func(_ context.Context, _ []string, out io.Writer) error {
			if !gate.Enabled() {
				fmt.Fprintln(out, "mode: off")
				return nil
			}
			cfg := gate.Config()
			fmt.Fprintf(out, "mode:            %s\n", cfg.Mode)
			fmt.Fprintf(out, "store:           %s\n", cfg.Store.Backend)
			fmt.Fprintf(out, "match:           score >= %d and >= %d kinds; cluster at %d\n", cfg.MinScore, cfg.MinKinds, cfg.ClusterScore)
			fmt.Fprintf(out, "fan-out demote:  digests on more than %d machines\n", cfg.FanOutLimit)
			fmt.Fprintf(out, "virtual machine: %s\n", cfg.VMPolicy)
			fmt.Fprintf(out, "hardware ban:    expires after %s\n", cfg.HardwareBanTTL.Duration())
			fmt.Fprintf(out, "require report:  %t\n", cfg.RequireReport)
			fmt.Fprintf(out, "require launcher for in-game login: %t\n", cfg.RequireLauncher)
			fmt.Fprintf(out, "require hardware-backed key:        %t\n", cfg.RequireHardwareKey)
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
