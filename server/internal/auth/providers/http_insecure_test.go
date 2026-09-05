package providers

import "testing"

func TestInsecureURLDetection(t *testing.T) {
	insecure := []string{
		"http://site.example/api/auth",
		"http://203.0.113.7:5000/login",
	}
	for _, raw := range insecure {
		if !sendsPasswordsInClear(raw) {
			t.Fatalf("http на внешний адрес должен вызывать предупреждение: %s", raw)
		}
	}

	safe := []string{
		"https://site.example/api/auth",
		"http://localhost:5000/login",
		"http://127.0.0.1/auth",
		"http://[::1]:8080/auth",
	}
	for _, raw := range safe {
		if sendsPasswordsInClear(raw) {
			t.Fatalf("этот URL безопасен, предупреждать не нужно: %s", raw)
		}
	}
}
