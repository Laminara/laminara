package pathpolicy

import (
	"strings"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
)

func Match(pattern, path string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if strings.HasSuffix(pattern, "/") {
		pattern += "**"
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

func matchSegments(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if matchSegments(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	if !matchSegment(pat[0], name[0]) {
		return false
	}
	return matchSegments(pat[1:], name[1:])
}

func matchSegment(pattern, s string) bool {
	star, mark, p, i := -1, 0, 0, 0
	for i < len(s) {
		if p < len(pattern) && pattern[p] == s[i] {
			p++
			i++
		} else if p < len(pattern) && pattern[p] == '*' {
			star, mark = p, i
			p++
		} else if star != -1 {
			p = star + 1
			mark++
			i = mark
		} else {
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}

func Resolve(path string, userWritable, enforced []string) corev1.FilePolicy {
	for _, pattern := range enforced {
		if Match(pattern, path) {
			return corev1.FilePolicy_FILE_POLICY_ENFORCED
		}
	}
	for _, pattern := range userWritable {
		if Match(pattern, path) {
			return corev1.FilePolicy_FILE_POLICY_USER_WRITABLE
		}
	}
	return corev1.FilePolicy_FILE_POLICY_UNSPECIFIED
}
