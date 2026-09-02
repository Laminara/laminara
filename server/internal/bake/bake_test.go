package bake

import (
	"bytes"
	"encoding/binary"
	"testing"
)

var template = []byte("pretend this is a launcher binary")

func TestAttachThenRead(t *testing.T) {
	config := []byte(`{"endpoints":[{"id":"main","baseUrl":"https://example"}]}`)

	image, err := Attach(template, config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(image, template) {
		t.Fatal("the launcher itself must stay untouched in front of the config")
	}
	got, ok := Read(image)
	if !ok || !bytes.Equal(got, config) {
		t.Fatalf("read back %q, ok=%v", got, ok)
	}
	if !Baked(image) || !Holds(image, config) {
		t.Fatal("a baked image must report the config it carries")
	}
}

func TestRebakeReplacesTheOldConfig(t *testing.T) {
	first, err := Attach(template, []byte(`{"first":true}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Attach(first, []byte(`{"second":true}`))
	if err != nil {
		t.Fatal(err)
	}

	got, ok := Read(second)
	if !ok || string(got) != `{"second":true}` {
		t.Fatalf("read back %q, ok=%v", got, ok)
	}
	if !bytes.Equal(Strip(second), template) {
		t.Fatalf("stripping a twice-baked image must leave the template alone, got %q", Strip(second))
	}
	if len(second) != len(template)+len(`{"second":true}`)+trailerLen {
		t.Fatalf("rebaking must not stack trailers: %d bytes", len(second))
	}
}

func TestPlainBinaryCarriesNoConfig(t *testing.T) {
	if _, ok := Read(template); ok {
		t.Fatal("a plain binary must not look baked")
	}
	if !bytes.Equal(Strip(template), template) {
		t.Fatal("stripping a plain binary must change nothing")
	}
	if _, ok := Read(nil); ok {
		t.Fatal("an empty image must not look baked")
	}
}

func TestTrailerThatLiesIsRefused(t *testing.T) {
	image := append([]byte("short"), make([]byte, 0, trailerLen)...)
	image = binary.LittleEndian.AppendUint64(image, ^uint64(0))
	image = append(image, magic...)

	if _, ok := Read(image); ok {
		t.Fatal("a length larger than the file must be refused instead of panicking")
	}
}

func TestEmptyAndOversizedConfigsAreRefused(t *testing.T) {
	if _, err := Attach(template, nil); err == nil {
		t.Fatal("an empty config must be refused")
	}
	if _, err := Attach(template, make([]byte, maxConfig+1)); err == nil {
		t.Fatal("an oversized config must be refused")
	}
}
