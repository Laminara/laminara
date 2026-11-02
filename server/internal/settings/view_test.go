package settings

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactedHeadersKeepSecretsWhileEditing(t *testing.T) {
	doc, err := Open(write(t, `{"auth":{"provider":"http","config":{"url":"https://example.test/auth","headers":{"Authorization":"Bearer fixture","Accept":"text/plain, application/json"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := doc.Entry("auth.config.headers")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(entry.Value, "fixture") {
		t.Fatal("secret returned to the editor")
	}
	if err := doc.Edit("auth.config.headers", entry.Value); err != nil {
		t.Fatal(err)
	}
	if err := doc.Edit("auth.config.headers", `{"Authorization":"***","Accept":"application/xml"}`); err != nil {
		t.Fatal(err)
	}
	value, _ := doc.raw("auth.config.headers.Authorization")
	if value != "Bearer fixture" {
		t.Fatalf("stored secret changed: %v", value)
	}
	value, _ = doc.raw("auth.config.headers.Accept")
	if value != "application/xml" {
		t.Fatalf("public header not updated: %v", value)
	}
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Open(doc.Path())
	if err != nil {
		t.Fatal(err)
	}
	value, _ = reloaded.raw("auth.config.headers.Authorization")
	if value != "Bearer fixture" {
		t.Fatal("secret changed after saving")
	}
}

func TestSecretReplacementAndClearingAreExplicit(t *testing.T) {
	doc, err := Open(write(t, `{"auth":{"provider":"http","config":{"headers":{"Authorization":"old"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Set("auth.config.headers", `{"Authorization":"***"}`); err != nil {
		t.Fatal(err)
	}
	value, _ := doc.raw("auth.config.headers.Authorization")
	if value != SecretMask {
		t.Fatal("literal replacement must accept the mask as a real value")
	}
	if err := doc.Edit("auth.config.headers", `{"Accept":"application/json"}`); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.raw("auth.config.headers.Authorization"); ok {
		t.Fatal("removed header retained")
	}
	before, _ := doc.Bytes()
	if err := doc.Edit("auth.config.headers", `{"Authorization":"***"}`); err == nil {
		t.Fatal("cannot preserve an absent secret")
	}
	after, _ := doc.Bytes()
	if string(after) != string(before) {
		t.Fatal("failed edit changed document")
	}
}

func TestParentAndOpaqueSettingsNeverExposeSecrets(t *testing.T) {
	for _, test := range []struct {
		name, body, path string
	}{
		{"http", `{"auth":{"provider":"http","config":{"headers":{"Authorization":"fixture-secret"}}}}`, "auth.config"},
		{"sink", `{"crashReports":{"sinks":{"demo":{"type":"http","config":{"headers":{"Authorization":"fixture-secret"}}}}}}`, "crashReports.sinks.demo"},
		{"module", `{"modules":{"config":{"demo":{"unexpected":["fixture-secret"]}}}}`, "modules.config.demo"},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, err := Open(write(t, test.body))
			if err != nil {
				t.Fatal(err)
			}
			before, _ := doc.Bytes()
			entry, err := doc.Entry(test.path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(entry.Value+entry.Display+entry.Default, "fixture-secret") {
				t.Fatal("secret visible in parent entry")
			}
			if err := doc.Edit(test.path, entry.Value); err != nil {
				t.Fatal(err)
			}
			after, _ := doc.Bytes()
			if string(before) != string(after) {
				t.Fatal("reading and saving masked entry changed the document")
			}
		})
	}
}

func TestChangingProviderDoesNotReuseMaskedCredentials(t *testing.T) {
	doc, err := Open(write(t, `{"crashReports":{"sinks":{"demo":{"type":"http","config":{"url":"https://example.test","headers":{"Authorization":"fixture"}}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Edit("crashReports.sinks.demo", `{"type":"discord","config":{"webhook":"***"}}`); err == nil {
		t.Fatal("changed adapter accepted masked credential")
	}
}

func TestPairsRoundTripPreservesPunctuation(t *testing.T) {
	doc, err := Open(write(t, `{"auth":{"provider":"http","config":{"headers":{"Accept":"text/plain, application/json","X-Label":"a=b, c"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := doc.Entry("auth.config.headers")
	if err != nil {
		t.Fatal(err)
	}
	var pairs map[string]string
	if err := json.Unmarshal([]byte(entry.Value), &pairs); err != nil {
		t.Fatalf("editable pairs must be JSON: %v", err)
	}
	if pairs["Accept"] != "text/plain, application/json" || pairs["X-Label"] != SecretMask {
		t.Fatalf("pairs changed: %v", pairs)
	}
	if err := doc.Edit("auth.config.headers", entry.Value); err != nil {
		t.Fatal(err)
	}
	value, _ := doc.raw("auth.config.headers.X-Label")
	if value != "a=b, c" {
		t.Fatal("masked custom header lost punctuation")
	}
}

func TestOnlyConsoleSessionDurationAllowsZero(t *testing.T) {
	doc, err := Open(write(t, `{}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"0", "0s"} {
		if err := doc.Set("console.sessionTTL", value); err != nil {
			t.Fatalf("no-expiry value %s rejected: %v", value, err)
		}
	}
	for _, path := range []string{"auth.accessTTL", "auth.refreshTTL"} {
		if err := doc.Set(path, "0s"); err == nil {
			t.Fatalf("%s must remain positive", path)
		}
	}
}
