package hash

import (
	"encoding/hex"
	"strconv"
	"strings"
)

type Detected struct {
	Scheme     string
	Cost       int
	Parameters string
	Supported  bool
}

func Detect(stored string) (Detected, bool) {
	trimmed := strings.TrimSpace(stored)
	if trimmed == "" {
		return Detected{}, false
	}
	if strings.HasPrefix(trimmed, "$argon2") {
		return detectArgon(trimmed)
	}
	if strings.HasPrefix(trimmed, "$2") {
		return detectBcrypt(trimmed)
	}
	if scheme, ok := detectHex(trimmed); ok {
		return Detected{Scheme: scheme, Supported: true}, true
	}
	return Detected{}, false
}

func detectArgon(stored string) (Detected, bool) {
	parts := strings.Split(stored, "$")
	if len(parts) < 4 {
		return Detected{}, false
	}
	scheme := parts[1]
	parameters := ""
	for _, part := range parts[2:] {
		if strings.HasPrefix(part, "m=") {
			parameters = part
			break
		}
	}
	return Detected{
		Scheme:     scheme,
		Parameters: parameters,
		Supported:  scheme == "argon2id",
	}, true
}

func detectBcrypt(stored string) (Detected, bool) {
	parts := strings.Split(stored, "$")
	if len(parts) < 4 {
		return Detected{}, false
	}
	variant := parts[1]
	if variant != "2" && variant != "2a" && variant != "2b" && variant != "2x" && variant != "2y" {
		return Detected{}, false
	}
	cost, err := strconv.Atoi(parts[2])
	if err != nil {
		return Detected{}, false
	}
	return Detected{Scheme: "bcrypt", Cost: cost, Supported: true}, true
}

func detectHex(stored string) (string, bool) {
	lowered := strings.ToLower(stored)
	if _, err := hex.DecodeString(lowered); err != nil {
		return "", false
	}
	switch len(lowered) {
	case 32:
		return "md5", true
	case 64:
		return "sha256", true
	case 128:
		return "sha512", true
	}
	return "", false
}
