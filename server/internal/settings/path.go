package settings

import (
	"net/url"
	"strings"
)

func pathKey(key string) string {
	return strings.ReplaceAll(url.PathEscape(key), ".", "%2E")
}

func unescapeKey(key string) string {
	decoded, err := url.PathUnescape(key)
	if err != nil {
		return key
	}
	return decoded
}
