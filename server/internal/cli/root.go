package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const usageTemplate = `Использование:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [команда]{{end}}{{if gt (len .Aliases) 0}}

Другие имена:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Пример:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

Команды:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Ключи:
{{ключи .LocalFlags}}{{end}}{{if .HasAvailableInheritedFlags}}

Общие ключи:
{{ключи .InheritedFlags}}{{end}}{{if .HasAvailableSubCommands}}

Подробности о команде: {{.CommandPath}} [команда] --help{{end}}
`

const helpTemplate = `{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`

func Root() *cobra.Command {
	root := &cobra.Command{
		Use:           "laminara-server",
		Short:         "Сервер Laminara: демон и управление им",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		startCmd(),
		stopCmd(),
		statusCmd(),
		consoleCmd(),
		logsCmd(),
		execCmd(),
		hashCmd(),
		clientConfigCmd(),
		nginxConfigCmd(),
		updateCmd(),
		versionCmd(),
	)
	speakRussian(root)
	return root
}

func speakRussian(root *cobra.Command) {
	cobra.AddTemplateFunc("ключи", flagUsages)
	root.SetUsageTemplate(usageTemplate)
	root.SetHelpTemplate(helpTemplate)
	root.DisableFlagsInUseLine = true
	for _, cmd := range root.Commands() {
		cmd.DisableFlagsInUseLine = true
	}
	root.PersistentFlags().BoolP("help", "h", false, "справка по команде")
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	for _, cmd := range root.Commands() {
		switch cmd.Name() {
		case "help":
			cmd.Use = "help [команда]"
			cmd.Short = "справка по команде"
			cmd.Long = ""
		case "completion":
			cmd.DisableFlagsInUseLine = true
			cmd.Short = "автодополнение команд в оболочке"
			cmd.Long = ""
			for _, shell := range cmd.Commands() {
				shell.Short = "автодополнение для " + shell.Name()
				shell.Long = ""
			}
		}
	}
}

var flagTypes = map[string]string{
	"string":      "строка",
	"stringArray": "строка",
	"int":         "число",
	"uint":        "число",
	"uint32":      "число",
	"int64":       "число",
	"duration":    "время",
}

func flagUsages(set *pflag.FlagSet) string {
	type row struct{ head, help string }
	var rows []row
	width := 0
	set.VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden {
			return
		}
		kind, usage := pflag.UnquoteUsage(flag)
		head := "      --" + flag.Name
		if flag.Shorthand != "" {
			head = "  -" + flag.Shorthand + ", --" + flag.Name
		}
		if word, ok := flagTypes[kind]; ok {
			head += " " + word
		} else if kind != "" {
			head += " " + kind
		}
		if flag.DefValue != "" && flag.DefValue != "false" && flag.DefValue != "0" && flag.DefValue != "[]" {
			usage += fmt.Sprintf(" (по умолчанию %s)", flag.DefValue)
		}
		rows = append(rows, row{head: head, help: usage})
		width = max(width, len([]rune(head)))
	})
	lines := make([]string, 0, len(rows))
	for _, entry := range rows {
		lines = append(lines, entry.head+strings.Repeat(" ", width-len([]rune(entry.head)))+"   "+entry.help)
	}
	return strings.Join(lines, "\n")
}
