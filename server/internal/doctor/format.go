package doctor

import (
	"fmt"
	"io"
	"strings"

	"github.com/laminara/laminara/server/internal/diag"
	"github.com/laminara/laminara/server/internal/humanize"
)

const lineWidth = 78

func Write(out io.Writer, results []diag.Result) {
	titles := Titles()
	current := ""
	for _, result := range order(results) {
		if result.Section != current {
			current = result.Section
			writeHeading(out, titles[current])
		}
		writeResult(out, result)
	}
	writeSummary(out, results)
}

func writeHeading(out io.Writer, title string) {
	if title == "" {
		return
	}
	rule := lineWidth - len([]rune(title)) - 1
	if rule < 3 {
		rule = 3
	}
	fmt.Fprintf(out, "\n%s %s\n", title, strings.Repeat("─", rule))
}

func writeResult(out io.Writer, result diag.Result) {
	fmt.Fprintf(out, "  %-6s %-24s %s\n", mark(result.Verdict), result.What, result.Detail)
	if result.Verdict == diag.OK || result.Verdict == diag.Skipped {
		return
	}
	if result.Remedy.Hint != "" {
		for _, line := range wrap(result.Remedy.Hint, lineWidth-11) {
			fmt.Fprintf(out, "         %s\n", line)
		}
	}
	if result.Remedy.Command != "" {
		fmt.Fprintf(out, "         → %s\n", result.Remedy.Command)
	}
}

func mark(verdict diag.Verdict) string {
	switch verdict {
	case diag.Warn:
		return "важно"
	case diag.Fail:
		return "плохо"
	case diag.Skipped:
		return "  —  "
	default:
		return "  ok "
	}
}

func writeSummary(out io.Writer, results []diag.Result) {
	ok, warn, fail, skipped := diag.Tally(results)
	total := ok + warn + fail
	fmt.Fprintf(out, "\nИтог: %s — %d ok", humanize.Count(total, "проверка", "проверки", "проверок"), ok)
	if warn > 0 {
		fmt.Fprintf(out, ", %d важно", warn)
	}
	if fail > 0 {
		fmt.Fprintf(out, ", %d плохо", fail)
	}
	if skipped > 0 {
		fmt.Fprintf(out, ", %d пропущено", skipped)
	}
	fmt.Fprintln(out)
	if fixable := diag.Fixable(results); fixable > 0 {
		fmt.Fprintf(out, "Исправляется само: %d — запустите doctor --fix\n", fixable)
	}
	switch {
	case fail > 0:
		fmt.Fprintln(out, "Сервер в таком виде работать не будет — сначала разберитесь со строками «плохо».")
	case warn > 0:
		fmt.Fprintln(out, "Сервер работает, но строки «важно» однажды принесут беду.")
	default:
		fmt.Fprintln(out, "Всё в порядке.")
	}
}

func wrap(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	line := words[0]
	for _, word := range words[1:] {
		if len([]rune(line))+len([]rune(word))+1 > width {
			lines = append(lines, line)
			line = word
			continue
		}
		line += " " + word
	}
	return append(lines, line)
}

type jsonResult struct {
	Section string `json:"section"`
	What    string `json:"what"`
	Verdict string `json:"verdict"`
	Detail  string `json:"detail"`
	Hint    string `json:"hint,omitempty"`
	Command string `json:"command,omitempty"`
	Fixable bool   `json:"fixable,omitempty"`
}

func JSON(results []diag.Result) any {
	ok, warn, fail, skipped := diag.Tally(results)
	items := make([]jsonResult, 0, len(results))
	for _, result := range order(results) {
		items = append(items, jsonResult{
			Section: result.Section,
			What:    result.What,
			Verdict: verdictName(result.Verdict),
			Detail:  result.Detail,
			Hint:    result.Remedy.Hint,
			Command: result.Remedy.Command,
			Fixable: result.Remedy.Apply != nil,
		})
	}
	return map[string]any{
		"verdict": verdictName(diag.Worst(results)),
		"checks":  items,
		"summary": map[string]int{"ok": ok, "warn": warn, "fail": fail, "skipped": skipped},
	}
}

func verdictName(verdict diag.Verdict) string {
	switch verdict {
	case diag.Warn:
		return "warn"
	case diag.Fail:
		return "fail"
	case diag.Skipped:
		return "skipped"
	default:
		return "ok"
	}
}
