package providers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/laminara/laminara/server/internal/auth"
)

func fileProvider(t *testing.T, records string) (*jsonFileProvider, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "users.json")
	if err := os.WriteFile(path, []byte(records), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"path": path, "hash": "argon2id"})
	provider, err := newJSONFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	return provider.(*jsonFileProvider), path
}

func TestAPlayerAddedByCommandCanSignInAtOnce(t *testing.T) {
	ctx := context.Background()
	provider, _ := fileProvider(t, "[]")

	identity, err := provider.Add(ctx, "Neo", "тайна", "")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Username != "Neo" || identity.UUID == (auth.Identity{}).UUID {
		t.Fatalf("игрок заведён неправильно: %+v", identity)
	}

	got, err := provider.Authenticate(ctx, auth.Credentials{Username: "Neo", Password: "тайна"})
	if err != nil {
		t.Fatalf("только что заведённый игрок не вошёл: %v", err)
	}
	if got.UUID != identity.UUID {
		t.Fatalf("uuid при входе другой: %s против %s", got.UUID, identity.UUID)
	}
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "Neo", Password: "не тайна"}); err == nil {
		t.Fatal("чужой пароль подошёл")
	}
}

func TestTheSameNickIsNotTakenTwice(t *testing.T) {
	ctx := context.Background()
	provider, _ := fileProvider(t, "[]")
	if _, err := provider.Add(ctx, "Neo", "раз", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Add(ctx, "Neo", "два", ""); err == nil {
		t.Fatal("второй игрок с тем же ником затёр бы первого")
	}
}

func TestPasswordChangeAndRemovalTakeEffect(t *testing.T) {
	ctx := context.Background()
	provider, _ := fileProvider(t, "[]")
	if _, err := provider.Add(ctx, "Neo", "старый", ""); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetPassword(ctx, "Neo", "новый"); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "Neo", Password: "старый"}); err == nil {
		t.Fatal("старый пароль продолжает пускать")
	}
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "Neo", Password: "новый"}); err != nil {
		t.Fatalf("новый пароль не пускает: %v", err)
	}
	if err := provider.Remove(ctx, "Neo"); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "Neo", Password: "новый"}); err == nil {
		t.Fatal("удалённый игрок всё ещё входит")
	}
}

func TestAFileEditedByHandIsPickedUpWithoutRestart(t *testing.T) {
	ctx := context.Background()
	provider, path := fileProvider(t, "[]")
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "Trinity", Password: "пароль"}); err == nil {
		t.Fatal("в пустом файле никого нет")
	}

	digest, err := provider.hashed("пароль")
	if err != nil {
		t.Fatal(err)
	}
	edited := `[{"username": "Trinity", "password": ` + asJSON(digest) + `}]`
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "Trinity", Password: "пароль"}); err != nil {
		t.Fatalf("правка файла руками не подхватилась без перезапуска: %v", err)
	}
}

func TestUnknownFieldsOfARecordSurviveAnEdit(t *testing.T) {
	ctx := context.Background()
	provider, path := fileProvider(t, `[{"username": "Neo", "password": "x", "заметка": "владелец"}]`)
	if err := provider.SetPassword(ctx, "Neo", "новый"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var records []map[string]any
	if err := json.Unmarshal(raw, &records); err != nil {
		t.Fatal(err)
	}
	if records[0]["заметка"] != "владелец" {
		t.Fatalf("чужое поле записи потерялось: %+v", records[0])
	}
}

func asJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
