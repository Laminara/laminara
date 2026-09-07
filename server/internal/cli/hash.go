package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/laminara/laminara/server/internal/auth/hash"
)

func hashCmd() *cobra.Command {
	var algo string
	var cost int
	cmd := &cobra.Command{
		Use:   "hash [пароль]",
		Short: "посчитать хеш пароля для хранилища аккаунтов (без аргумента читает ввод)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var password string
			if len(args) == 1 {
				password = args[0]
			} else {
				line, err := bufio.NewReader(os.Stdin).ReadString('\n')
				if err != nil && line == "" {
					return err
				}
				password = strings.TrimRight(line, "\r\n")
			}
			if cost > 0 && !hash.AcceptsCost(algo) {
				return fmt.Errorf("схема %q не принимает --cost, стоимость задаётся только для bcrypt", algo)
			}
			digest, err := hash.ProduceCost(algo, password, cost)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), digest)
			return nil
		},
	}
	cmd.Flags().StringVar(&algo, "algo", "argon2id", "схема хеширования (argon2id, bcrypt, sha256, sha512, md5, plain)")
	cmd.Flags().IntVar(&cost, "cost", 0, fmt.Sprintf("стоимость bcrypt от 4 до 31 (по умолчанию %d)", hash.BcryptDefaultCost))
	return cmd
}
