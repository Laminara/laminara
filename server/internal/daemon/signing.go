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
		Synopsis: "show the signing keyring (signing keys | signing new <path>)",
		Run: func(_ context.Context, args []string, out io.Writer) error {
			if ring == nil {
				return errors.New("no signing key is configured")
			}
			if len(args) == 0 {
				args = []string{"keys"}
			}
			switch args[0] {
			case "keys":
				trusted := ring.TrustedHex()
				fmt.Fprintf(out, "active:  %s\n", ring.ActiveHex())
				for _, key := range trusted[1:] {
					fmt.Fprintf(out, "trusted: %s\n", key)
				}
				if len(trusted) == 1 {
					fmt.Fprintln(out, "\nOnly one key is trusted. Launchers built now can never move to another")
					fmt.Fprintln(out, "key: run \"signing new <path>\" before you ever need to rotate.")
				}
				return nil
			case "new":
				if len(args) < 2 {
					return errors.New("usage: signing new <path>")
				}
				key, err := signing.Generate(args[1])
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "wrote %s\npublic: %s\n\n", args[1], signing.PublicKeyHex(key))
				fmt.Fprintln(out, "Rotate in this order, or launchers in the field will stop trusting you:")
				fmt.Fprintf(out, "  1. add %q to build.trustedSigningKeys and restart\n", args[1])
				fmt.Fprintln(out, "  2. laminara-server client-config ... > laminara.client.json, rebuild the launcher")
				fmt.Fprintln(out, "  3. launcher publish <version>  (still signed by the current key, so everyone accepts it)")
				fmt.Fprintln(out, "  4. wait until players are on that release")
				fmt.Fprintln(out, "  5. make it build.signingKeyPath, move the old key to trustedSigningKeys, restart")
				fmt.Fprintln(out, "  6. much later, drop the old key and rebuild once more")
				return nil
			default:
				return fmt.Errorf("unknown signing subcommand %q", args[0])
			}
		},
	}
}
