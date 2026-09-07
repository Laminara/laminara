package settings

import "testing"

func TestHeaderSecretsAreMasked(t *testing.T) {
	pairs := map[string]any{
		"Authorization": "Bearer очень-секретно",
		"X-Api-Key":     "ключ",
		"Accept":        "application/json",
	}
	got := render(KindPairs, pairs)
	if contains(got, "очень-секретно") || contains(got, "ключ") {
		t.Fatalf("секрет виден в выводе настроек: %s", got)
	}
	if !contains(got, "Accept=application/json") {
		t.Fatalf("обычный заголовок должен показываться целиком: %s", got)
	}
	if !contains(got, "Authorization="+SecretMask) {
		t.Fatalf("вместо секрета должна стоять маска: %s", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && stringIndex(haystack, needle) >= 0
}

func stringIndex(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
