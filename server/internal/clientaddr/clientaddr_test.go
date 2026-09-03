package clientaddr

import (
	"net/http"
	"testing"
)

func headers(values ...string) http.Header {
	header := http.Header{}
	for _, value := range values {
		header.Add("X-Forwarded-For", value)
	}
	return header
}

func TestHeadersFromStrangersAreIgnored(t *testing.T) {
	trust, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}

	got := trust.Of(headers("1.2.3.4"), "203.0.113.9:51000")
	if got != "203.0.113.9" {
		t.Fatalf("адрес = %q, а заголовку от постороннего верить нельзя: он снимает счётчик попыток входа", got)
	}
}

func TestHeaderFromATrustedProxyIsHonoured(t *testing.T) {
	trust, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}

	if got := trust.Of(headers("1.2.3.4"), "127.0.0.1:9000"); got != "1.2.3.4" {
		t.Fatalf("адрес = %q, ждали адрес игрока из заголовка", got)
	}
	if got := trust.Of(headers("1.2.3.4"), "172.18.0.5:9000"); got != "1.2.3.4" {
		t.Fatalf("докерная сеть считается доверенной: %q", got)
	}
}

func TestTheLastUntrustedHopWins(t *testing.T) {
	trust, err := New([]string{"127.0.0.0/8", "10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}

	got := trust.Of(headers("9.9.9.9, 1.2.3.4", "10.0.0.7"), "127.0.0.1:443")
	if got != "1.2.3.4" {
		t.Fatalf("адрес = %q: подставленные слева адреса не должны перебивать реальный", got)
	}
}

func TestEmptyListTrustsNobody(t *testing.T) {
	trust, err := New([]string{})
	if err != nil {
		t.Fatal(err)
	}

	if got := trust.Of(headers("1.2.3.4"), "127.0.0.1:9000"); got != "127.0.0.1" {
		t.Fatalf("адрес = %q, при пустом списке заголовки не читаются вовсе", got)
	}
}

func TestSingleAddressesAndBadNetworks(t *testing.T) {
	trust, err := New([]string{"198.51.100.7"})
	if err != nil {
		t.Fatal(err)
	}
	if got := trust.Of(headers("1.2.3.4"), "198.51.100.7:8080"); got != "1.2.3.4" {
		t.Fatalf("адрес без маски = один доверенный прокси, получили %q", got)
	}
	if _, err := New([]string{"не сеть"}); err == nil {
		t.Fatal("мусор в списке прокси должен останавливать запуск, а не молча игнорироваться")
	}
}

func TestPeerWithoutPortStillWorks(t *testing.T) {
	trust, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := trust.Of(http.Header{}, "203.0.113.9"); got != "203.0.113.9" {
		t.Fatalf("адрес = %q", got)
	}
}
