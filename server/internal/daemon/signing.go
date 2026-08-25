package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/signing"
)

func signingCommand(ring *signing.Keyring) command.Command {
	return command.Command{
		Name:     "signing",
		Synopsis: "ключи подписи (signing keys | signing new <путь>)",
		Run: func(_ context.Context, args []string, out io.Writer) error {
			if ring == nil {
				return errors.New("ключ подписи не настроен — задайте build.signingKeyPath")
			}
			if len(args) == 0 {
				args = []string{"keys"}
			}
			switch args[0] {
			case "keys":
				trusted := ring.TrustedHex()
				fmt.Fprintf(out, "рабочий:   %s\n", ring.ActiveHex())
				for _, key := range trusted[1:] {
					fmt.Fprintf(out, "доверенный: %s\n", key)
				}
				if len(trusted) == 1 {
					fmt.Fprintln(out, "\nДоверенный ключ только один. Лаунчеры, собранные сейчас, никогда не примут другой")
					fmt.Fprintln(out, "ключ — заведите запасной заранее: signing new <путь>")
				}
				return nil
			case "new":
				if len(args) < 2 {
					return errors.New("как пользоваться: signing new <путь>")
				}
				key, err := signing.Generate(args[1])
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Ключ записан: %s\nоткрытая часть: %s\n\n", args[1], signing.PublicKeyHex(key))
				fmt.Fprintln(out, "Меняйте ключ строго в этом порядке, иначе лаунчеры у игроков перестанут вам верить:")
				fmt.Fprintf(out, "  1. добавьте %q в build.trustedSigningKeys и перезапустите проект\n", args[1])
				fmt.Fprintln(out, "  2. соберите новую конфигурацию: laminara-server client-config … > laminara.client.json, пересоберите лаунчер")
				fmt.Fprintln(out, "  3. launcher publish <версия> — она подписана ещё нынешним ключом, её примут все")
				fmt.Fprintln(out, "  4. дождитесь, пока игроки обновятся на эту версию")
				fmt.Fprintln(out, "  5. сделайте новый ключ рабочим (build.signingKeyPath), старый перенесите в trustedSigningKeys, перезапустите")
				fmt.Fprintln(out, "  6. много позже уберите старый ключ и пересоберите лаунчер ещё раз")
				return nil
			default:
				return fmt.Errorf("не знаю подкоманду signing %q", args[0])
			}
		},
	}
}
