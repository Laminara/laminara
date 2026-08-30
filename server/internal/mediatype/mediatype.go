package mediatype

import (
	"path/filepath"
	"strings"
)

const (
	Default      = "application/octet-stream"
	DefaultImage = "image/png"
)

type kind struct {
	ext  string
	name string
}

var known = []kind{
	{".svg", "image/svg+xml"},
	{".png", "image/png"},
	{".jpg", "image/jpeg"},
	{".jpeg", "image/jpeg"},
	{".webp", "image/webp"},
	{".gif", "image/gif"},
	{".mp4", "video/mp4"},
	{".webm", "video/webm"},
}

func Guess(name, declared string) string {
	if found := byExt(name); found != "" {
		return found
	}
	if found := byDeclared(declared); found != "" {
		return found
	}
	return Default
}

func GuessImage(name, declared string) string {
	if found := byExt(name); isImage(found) {
		return found
	}
	if found := byDeclared(declared); isImage(found) {
		return found
	}
	return DefaultImage
}

func isImage(mime string) bool {
	return strings.HasPrefix(mime, "image/")
}

func byExt(name string) string {
	extension := strings.ToLower(filepath.Ext(strings.SplitN(name, "?", 2)[0]))
	for _, item := range known {
		if item.ext == extension {
			return item.name
		}
	}
	return ""
}

func byDeclared(declared string) string {
	declared = strings.ToLower(strings.TrimSpace(declared))
	for _, item := range known {
		if strings.HasPrefix(declared, item.name) {
			return item.name
		}
	}
	return ""
}
