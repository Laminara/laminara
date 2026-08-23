package platform

import (
	"strings"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
)

var mojang = map[corev1.Platform][2]string{
	corev1.Platform_PLATFORM_WINDOWS_X64:   {"windows", "x86_64"},
	corev1.Platform_PLATFORM_WINDOWS_X86:   {"windows", "x86"},
	corev1.Platform_PLATFORM_WINDOWS_ARM64: {"windows", "arm64"},
	corev1.Platform_PLATFORM_LINUX:         {"linux", "x86_64"},
	corev1.Platform_PLATFORM_LINUX_I386:    {"linux", "x86"},
	corev1.Platform_PLATFORM_MAC_OS:        {"osx", "x86_64"},
	corev1.Platform_PLATFORM_MAC_OS_ARM64:  {"osx", "arm64"},
}

func Key(p corev1.Platform) (string, bool) {
	name, ok := corev1.Platform_name[int32(p)]
	if !ok || p == corev1.Platform_PLATFORM_UNSPECIFIED {
		return "", false
	}
	return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(name, "PLATFORM_")), "_", "-"), true
}

func Parse(key string) (corev1.Platform, bool) {
	want := strings.ToLower(strings.TrimSpace(key))
	for value := range corev1.Platform_name {
		p := corev1.Platform(value)
		if k, ok := Key(p); ok && k == want {
			return p, true
		}
	}
	return corev1.Platform_PLATFORM_UNSPECIFIED, false
}

func Game() []corev1.Platform {
	ordered := []corev1.Platform{
		corev1.Platform_PLATFORM_WINDOWS_X64,
		corev1.Platform_PLATFORM_WINDOWS_X86,
		corev1.Platform_PLATFORM_WINDOWS_ARM64,
		corev1.Platform_PLATFORM_LINUX,
		corev1.Platform_PLATFORM_LINUX_I386,
		corev1.Platform_PLATFORM_MAC_OS,
		corev1.Platform_PLATFORM_MAC_OS_ARM64,
	}
	return ordered
}

func Keys(platforms []corev1.Platform) []string {
	out := make([]string, 0, len(platforms))
	for _, p := range platforms {
		if key, ok := Key(p); ok {
			out = append(out, key)
		}
	}
	return out
}

func Mojang(p corev1.Platform) (string, string, bool) {
	pair, ok := mojang[p]
	if !ok {
		return "", "", false
	}
	return pair[0], pair[1], true
}

func FromRuntime(goos, goarch string) corev1.Platform {
	switch goos {
	case "windows":
		switch goarch {
		case "386":
			return corev1.Platform_PLATFORM_WINDOWS_X86
		case "arm64":
			return corev1.Platform_PLATFORM_WINDOWS_ARM64
		default:
			return corev1.Platform_PLATFORM_WINDOWS_X64
		}
	case "darwin":
		if goarch == "arm64" {
			return corev1.Platform_PLATFORM_MAC_OS_ARM64
		}
		return corev1.Platform_PLATFORM_MAC_OS
	default:
		switch goarch {
		case "386":
			return corev1.Platform_PLATFORM_LINUX_I386
		case "arm64":
			return corev1.Platform_PLATFORM_LINUX_ARM64
		default:
			return corev1.Platform_PLATFORM_LINUX
		}
	}
}
