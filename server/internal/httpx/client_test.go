package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/laminara/laminara/server/internal/httpx"
)

func TestEveryOutgoingRequestNamesItself(t *testing.T) {
	seen := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.UserAgent()
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := httpx.NewClient(5 * time.Second)
	var out map[string]any
	if err := httpx.GetJSON(context.Background(), client, server.URL, &out); err != nil {
		t.Fatal(err)
	}
	agent := <-seen
	if !strings.HasPrefix(agent, "laminara-server/") {
		t.Fatalf("User-Agent = %q: Maven Central отвечает 429 на клиентов, которые не назвались", agent)
	}

	request, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	request.Header.Set("User-Agent", "своё имя")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if agent := <-seen; agent != "своё имя" {
		t.Fatalf("заданный вручную User-Agent затёрт на %q", agent)
	}
}
