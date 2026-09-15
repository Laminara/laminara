package skin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fixedTextures struct {
	textures Textures
}

func (f fixedTextures) Textures(context.Context, string, string) (Textures, error) {
	return f.textures, nil
}

func pngBytes(marker byte) []byte {
	body := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, marker}
	return body
}

func TestMirrorNamesPictureByItsContent(t *testing.T) {
	image := pngBytes(1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(image)
	}))
	defer upstream.Close()

	mirror := newMirror(fixedTextures{Textures{SkinURL: upstream.URL + "/skins/steve.png"}},
		"https://play.example/yggdrasil/textures", upstream.Client(), time.Minute, time.Now)

	textures, err := mirror.Textures(context.Background(), "Steve", "8667ba71-b85a-4004-af54-457a9734eed7")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(image)
	want := "https://play.example/yggdrasil/textures/" + hex.EncodeToString(sum[:])
	if textures.SkinURL != want {
		t.Fatalf("ссылка на скин должна кончаться хешем картинки: %s", textures.SkinURL)
	}

	recorder := httptest.NewRecorder()
	mirror.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, textures.SkinURL, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("зеркало не отдало картинку: %d", recorder.Code)
	}
	if recorder.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("зеркало отдаёт не PNG: %s", recorder.Header().Get("Content-Type"))
	}
	if string(recorder.Body.Bytes()) != string(image) {
		t.Fatal("зеркало отдало не те байты, что скачало")
	}
}

func TestMirrorChangesLinkWhenPictureChanges(t *testing.T) {
	image := pngBytes(1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(image)
	}))
	defer upstream.Close()

	moment := time.Now()
	mirror := newMirror(fixedTextures{Textures{SkinURL: upstream.URL + "/skins/steve.png"}},
		"https://play.example/yggdrasil/textures", upstream.Client(), time.Minute, func() time.Time { return moment })

	before, err := mirror.Textures(context.Background(), "Steve", "id")
	if err != nil {
		t.Fatal(err)
	}
	image = pngBytes(2)
	same, err := mirror.Textures(context.Background(), "Steve", "id")
	if err != nil {
		t.Fatal(err)
	}
	if same.SkinURL != before.SkinURL {
		t.Fatal("внутри минуты зеркало не должно ходить за картинкой заново")
	}
	moment = moment.Add(2 * time.Minute)
	after, err := mirror.Textures(context.Background(), "Steve", "id")
	if err != nil {
		t.Fatal(err)
	}
	if after.SkinURL == before.SkinURL {
		t.Fatal("сменилась картинка — обязана смениться и ссылка, иначе игра покажет старый скин из своего кэша")
	}
}

func TestMirrorKeepsLastPictureWhenSourceHiccups(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(pngBytes(1))
	}))
	moment := time.Now()
	mirror := newMirror(fixedTextures{Textures{SkinURL: upstream.URL + "/skins/steve.png"}},
		"https://play.example/yggdrasil/textures", upstream.Client(), time.Minute, func() time.Time { return moment })

	before, err := mirror.Textures(context.Background(), "Steve", "id")
	if err != nil {
		t.Fatal(err)
	}
	upstream.Close()
	moment = moment.Add(2 * time.Minute)
	after, err := mirror.Textures(context.Background(), "Steve", "id")
	if err != nil {
		t.Fatal(err)
	}
	if after.SkinURL != before.SkinURL {
		t.Fatalf("сайт со скинами моргнул — игрок не должен из-за этого терять скин: %s", after.SkinURL)
	}
}

func TestMirrorForgetsPictureThatSourceRemoved(t *testing.T) {
	removed := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if removed {
			http.Error(w, "нет такого", http.StatusNotFound)
			return
		}
		_, _ = w.Write(pngBytes(1))
	}))
	defer upstream.Close()

	raw := upstream.URL + "/skins/steve.png"
	moment := time.Now()
	mirror := newMirror(fixedTextures{Textures{SkinURL: raw}},
		"https://play.example/yggdrasil/textures", upstream.Client(), time.Minute, func() time.Time { return moment })

	if _, err := mirror.Textures(context.Background(), "Steve", "id"); err != nil {
		t.Fatal(err)
	}
	removed = true
	moment = moment.Add(2 * time.Minute)
	after, err := mirror.Textures(context.Background(), "Steve", "id")
	if err != nil {
		t.Fatal(err)
	}
	if after.SkinURL != raw {
		t.Fatalf("скин убрали — зеркало обязано отпустить старую картинку: %s", after.SkinURL)
	}
}

func TestMirrorKeepsOriginalLinkWhenPictureIsUnreachable(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "нет такого", http.StatusNotFound)
	}))
	defer upstream.Close()

	raw := upstream.URL + "/skins/steve.png"
	mirror := newMirror(fixedTextures{Textures{SkinURL: raw, CapeURL: ""}},
		"https://play.example/yggdrasil/textures", upstream.Client(), time.Minute, time.Now)

	textures, err := mirror.Textures(context.Background(), "Steve", "id")
	if err != nil {
		t.Fatal(err)
	}
	if textures.SkinURL != raw {
		t.Fatalf("недоступную картинку зеркало обязано оставить как есть: %s", textures.SkinURL)
	}
}

func TestMirrorIgnoresAnswersThatAreNotPictures(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>login page</html>"))
	}))
	defer upstream.Close()

	raw := upstream.URL + "/skins/steve.png"
	mirror := newMirror(fixedTextures{Textures{SkinURL: raw}},
		"https://play.example/yggdrasil/textures", upstream.Client(), time.Minute, time.Now)

	textures, err := mirror.Textures(context.Background(), "Steve", "id")
	if err != nil {
		t.Fatal(err)
	}
	if textures.SkinURL != raw {
		t.Fatalf("не PNG — зеркалить нечего: %s", textures.SkinURL)
	}
}

func TestMirrorLeavesItsOwnLinksAlone(t *testing.T) {
	base := "https://play.example/yggdrasil/textures"
	mirror := newMirror(fixedTextures{Textures{SkinURL: base + "/deadbeef"}}, base, http.DefaultClient, time.Minute, time.Now)
	textures, err := mirror.Textures(context.Background(), "Steve", "id")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(textures.SkinURL, "/deadbeef") {
		t.Fatalf("ссылку зеркала перезеркалили: %s", textures.SkinURL)
	}
}
