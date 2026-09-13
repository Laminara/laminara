package daemon

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/humanize"
)

func accounts(ctx context.Context, service *auth.Service, args []string, out io.Writer) error {
	store, ok := service.Provider().(auth.Accounts)
	if !ok {
		return fmt.Errorf("источник аккаунтов не умеет их заводить — игроки заводятся там, где вы их храните")
	}
	switch args[0] {
	case "list":
		identities, err := store.List(ctx)
		if err != nil {
			return err
		}
		if len(identities) == 0 {
			fmt.Fprintln(out, "Аккаунтов пока нет — завести: auth add <игрок>")
			return nil
		}
		for _, identity := range identities {
			fmt.Fprintf(out, "%-20s %s\n", identity.Username, identity.UUID)
		}
		fmt.Fprintf(out, "\nВсего %s.\n", humanize.Count(len(identities), "аккаунт", "аккаунта", "аккаунтов"))
		return nil
	case "add":
		if len(args) < 2 {
			return fmt.Errorf("напишите ник: auth add <игрок> [пароль] [uuid]")
		}
		password, invented := args[1], ""
		if len(args) > 2 {
			password = args[2]
		} else {
			generated, err := invent()
			if err != nil {
				return err
			}
			password, invented = generated, generated
		}
		wanted := ""
		if len(args) > 3 {
			wanted = args[3]
		}
		identity, err := store.Add(ctx, args[1], password, wanted)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Игрок «%s» заведён, uuid %s\n", identity.Username, identity.UUID)
		if invented != "" {
			fmt.Fprintf(out, "Пароль: %s\n", invented)
			fmt.Fprintln(out, "Передайте его игроку — здесь он больше не покажется.")
		}
		return nil
	case "passwd":
		if len(args) < 2 {
			return fmt.Errorf("напишите ник: auth passwd <игрок> [пароль]")
		}
		password, invented := "", ""
		if len(args) > 2 {
			password = args[2]
		} else {
			generated, err := invent()
			if err != nil {
				return err
			}
			password, invented = generated, generated
		}
		if err := store.SetPassword(ctx, args[1], password); err != nil {
			return err
		}
		fmt.Fprintf(out, "Пароль игрока «%s» заменён.\n", args[1])
		if invented != "" {
			fmt.Fprintf(out, "Новый пароль: %s\n", invented)
			fmt.Fprintln(out, "Передайте его игроку — здесь он больше не покажется.")
		}
		fmt.Fprintln(out, "Уже открытые сессии игрока продолжают работать, пока не истекут сами.")
		return nil
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("напишите ник: auth remove <игрок>")
		}
		if err := store.Remove(ctx, args[1]); err != nil {
			return err
		}
		fmt.Fprintf(out, "Игрок «%s» удалён.\n", args[1])
		return nil
	}
	return fmt.Errorf("у auth нет действия «%s»", args[0])
}

func invent() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
