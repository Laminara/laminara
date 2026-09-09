package providers

import (
	"fmt"
	"log/slog"

	"github.com/laminara/laminara/server/internal/auth/hash"
)

func verifierFor(scheme string) (hash.Verifier, error) {
	verifier, err := hash.Get(scheme)
	if err != nil {
		return nil, err
	}
	if reason, weak := hash.Weakness(scheme); weak {
		slog.Warn("слабая схема хранения паролей",
			"source", "auth",
			"схема", scheme,
			"почему", reason,
			"что делать", "перевести базу на argon2id или bcrypt",
		)
	}
	return verifier, nil
}

func verify(verifier hash.Verifier, scheme, password, stored string) (bool, error) {
	valid, err := verifier.Verify(password, stored)
	if err != nil {
		return false, fmt.Errorf("схема %q не прочитала хранимый пароль (%w) — укажите в auth.config.hash ту схему, которой хеширует ваша база", scheme, err)
	}
	return valid, nil
}

const decoyHash = "$argon2id$v=19$m=65536,t=3,p=2$bGFtaW5hcmEtZGVjb3k$0000000000000000000000000000000000000000000"

func spendSameTime(verifier hash.Verifier, password string) {
	_, _ = verifier.Verify(password, decoyHash)
}
