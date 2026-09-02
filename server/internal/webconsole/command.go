package webconsole

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/humanize"
)

func (s *Service) Commands() []command.Command {
	if s == nil {
		return nil
	}
	return []command.Command{{
		Name:       "web",
		Synopsis:   "ссылка на консоль в браузере (web | web link | web forget)",
		SecretArgs: true,
		Run:        s.run,
	}}
}

func (s *Service) run(_ context.Context, args []string, out io.Writer) error {
	action := "link"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
	}
	switch action {
	case "link":
		fmt.Fprintf(out, "Ссылка действует %s и открывается один раз:\n%s\n", humanize.Duration(s.linkTTL()), s.Link())
		if !s.secured() {
			fmt.Fprintln(out, "Соединение не шифруется — ссылку и консоль увидит любой, кто слушает сеть между вами и сервером.")
		}
		return nil
	case "status":
		fmt.Fprintf(out, "Вход       %s\n", modeWord(s.cfg.mode()))
		fmt.Fprintf(out, "Открыто    %s\n", humanize.Count(s.Sessions(), "сеанс", "сеанса", "сеансов"))
		fmt.Fprintf(out, "Сеанс      %s\n", sessionWord(s.sessionTTL()))
		if s.cfg.PublicURL == "" {
			fmt.Fprintln(out, "Адрес      не задан (console.publicUrl) — ссылку придётся дополнять руками")
		} else {
			fmt.Fprintf(out, "Адрес      %s%s/\n", strings.TrimRight(s.cfg.PublicURL, "/"), basePath)
		}
		return nil
	case "forget":
		fmt.Fprintf(out, "Закрыл %s\n", humanize.Count(s.Forget(), "сеанс", "сеанса", "сеансов"))
		return nil
	default:
		return fmt.Errorf("не знаю действия %q: web | web status | web forget", action)
	}
}

func (s *Service) secured() bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(s.cfg.PublicURL)), "https://")
}

func modeWord(mode string) string {
	switch mode {
	case "password":
		return "по паролю"
	case "both":
		return "по ссылке или паролю"
	default:
		return "только по ссылке"
	}
}

func sessionWord(ttl time.Duration) string {
	if ttl <= 0 {
		return "не истекает"
	}
	return "живёт " + humanize.Duration(ttl)
}
