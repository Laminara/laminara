package version

import (
	"strconv"
	"strings"
)

var Current = "0.0.0-dev"

const coreSegments = 3

func Compare(left, right string) int {
	leftCore, leftPre, _ := split(left)
	rightCore, rightPre, _ := split(right)

	for i := 0; i < len(leftCore) || i < len(rightCore); i++ {
		if diff := part(leftCore, i) - part(rightCore, i); diff != 0 {
			if diff < 0 {
				return -1
			}
			return 1
		}
	}

	switch {
	case leftPre == rightPre:
		return 0
	case leftPre == "":
		return 1
	case rightPre == "":
		return -1
	case leftPre < rightPre:
		return -1
	default:
		return 1
	}
}

func IsNewer(candidate, current string) bool {
	return Compare(candidate, current) > 0
}

func IsValid(value string) bool {
	if strings.Contains(value, "+") {
		return false
	}
	core, pre, hasPre := split(value)
	if len(core) != coreSegments {
		return false
	}
	if hasPre && pre == "" {
		return false
	}
	for _, part := range core {
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	return true
}

func split(value string) ([]string, string, bool) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(value), "v")
	trimmed, _, _ = strings.Cut(trimmed, "+")
	core, pre, hasPre := strings.Cut(trimmed, "-")
	return strings.Split(core, "."), pre, hasPre
}

func part(parts []string, index int) int {
	if index >= len(parts) {
		return 0
	}
	number, err := strconv.Atoi(parts[index])
	if err != nil {
		return 0
	}
	return number
}
