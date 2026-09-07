package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/laminara/laminara/server/internal/diag"
)

const probeSize = 1 << 10

func roundtrip(ctx context.Context, backend Backend, probe *diag.Probe) string {
	key := "doctor/probe-" + uuid.NewString()
	payload := make([]byte, probeSize)
	if _, err := rand.Read(payload); err != nil {
		probe.Warn("заливка", fmt.Sprintf("не удалось подготовить пробу: %v", err), diag.Remedy{
			Hint: "повторите проверку",
		})
		return ""
	}
	if err := backend.Put(ctx, key, bytes.NewReader(payload), int64(len(payload))); err != nil {
		probe.Fail("заливка", fmt.Sprintf("записать не удалось: %v", err), diag.Remedy{
			Hint: "проверьте права на запись: для файлового хранилища — владельца каталога, для S3 — политику ключа доступа",
		})
		return ""
	}
	defer func() {
		if err := backend.Delete(context.WithoutCancel(ctx), key); err != nil {
			probe.Warn("уборка пробы", fmt.Sprintf("%s не удалился: %v", key, err), diag.Remedy{
				Hint: "запись работает, а удаление нет — проверьте права на удаление; пробный объект придётся убрать вручную",
			})
		}
	}()

	size, exists, err := backend.Stat(ctx, key)
	switch {
	case err != nil:
		probe.Fail("заливка", fmt.Sprintf("записанный объект не читается: %v", err), diag.Remedy{
			Hint: "хранилище приняло запись, но не отдаёт её обратно — проверьте права на чтение",
		})
		return ""
	case !exists:
		probe.Fail("заливка", "записанный объект тут же исчез", diag.Remedy{
			Hint: "хранилище подтверждает запись, но не хранит её — проверьте, что бакет или каталог тот самый",
		})
		return ""
	case size != int64(len(payload)):
		probe.Fail("заливка", fmt.Sprintf("размер не сходится: записали %d, хранилище отдаёт %d", len(payload), size), diag.Remedy{
			Hint: "объекты портятся при записи — проверьте прокси или файловую систему между сервером и хранилищем",
		})
		return ""
	}

	reader, err := backend.Get(ctx, key)
	if err != nil {
		probe.Fail("заливка", fmt.Sprintf("скачать обратно не удалось: %v", err), diag.Remedy{
			Hint: "проверьте права на чтение",
		})
		return ""
	}
	got, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		probe.Fail("заливка", fmt.Sprintf("чтение оборвалось: %v", err), diag.Remedy{
			Hint: "связь с хранилищем рвётся на середине файла — проверьте сеть и таймауты прокси",
		})
		return ""
	}
	if !bytes.Equal(got, payload) {
		probe.Fail("заливка", "скачанные байты не совпали с записанными", diag.Remedy{
			Hint: "хранилище портит содержимое; игроки будут получать битые файлы и вечно перекачивать сборку",
		})
		return ""
	}
	probe.OK("заливка", "%d байт записаны, прочитаны и совпали", len(payload))
	return key
}

func (b *fsBackend) Check(ctx context.Context, probe *diag.Probe) {
	info, err := os.Stat(b.root)
	if err != nil {
		probe.Fail("каталог объектов", fmt.Sprintf("%s: %v", b.root, err), diag.Remedy{
			Hint:    "создайте каталог и отдайте его пользователю сервера",
			Command: fmt.Sprintf("mkdir -p %s", b.root),
			Apply: func(context.Context) error {
				return os.MkdirAll(b.root, 0o755)
			},
		})
		return
	}
	if !info.IsDir() {
		probe.Fail("каталог объектов", fmt.Sprintf("%s — это файл, а не каталог", b.root), diag.Remedy{
			Hint: "укажите каталог в storage.config.root",
		})
		return
	}
	probe.OK("каталог объектов", "%s", b.root)
	roundtrip(ctx, b, probe)
	b.checkReadable(probe)
}

func (b *fsBackend) checkReadable(probe *diag.Probe) {
	closed := ""
	scanned := 0
	err := filepath.WalkDir(b.root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		scanned++
		info, statErr := entry.Info()
		if statErr != nil {
			return nil
		}
		if info.Mode().Perm()&0o044 == 0 {
			closed = path
			return filepath.SkipAll
		}
		if scanned >= 64 {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil || scanned == 0 {
		return
	}
	if closed == "" {
		probe.OK("доступ nginx к объектам", "файлы открыты на чтение")
		return
	}
	root := b.root
	probe.Warn("доступ nginx к объектам", fmt.Sprintf("%s закрыт на чтение — при xAccel nginx отдаст игроку ошибку", closed), diag.Remedy{
		Hint:    "объекты, сложенные старыми версиями сервера, лежат с правами 0600; откройте их на чтение",
		Command: fmt.Sprintf("chmod -R u+rwX,go+rX %s", root),
		Apply: func(context.Context) error {
			return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				info, statErr := entry.Info()
				if statErr != nil {
					return statErr
				}
				mode := info.Mode().Perm()
				if entry.IsDir() {
					return os.Chmod(path, mode|0o755)
				}
				return os.Chmod(path, mode|0o644)
			})
		},
	})
}

func (b *s3Backend) Check(ctx context.Context, probe *diag.Probe) {
	started := time.Now()
	exists, err := b.client.BucketExists(ctx, b.bucket)
	if err != nil {
		probe.Fail("бакет", fmt.Sprintf("%s не отвечает: %v", b.endpoint, err), diag.Remedy{
			Hint: "проверьте endpoint, ключи доступа и то, что хранилище пускает сервер по сети",
		})
		return
	}
	if !exists {
		probe.Fail("бакет", fmt.Sprintf("%s нет в %s", b.bucket, b.endpoint), diag.Remedy{
			Hint: "создайте бакет или укажите существующий в storage.config.bucket; ключ доступа тоже должен видеть именно его",
		})
		return
	}
	probe.OK("бакет", "%s доступен за %s", b.bucket, time.Since(started).Round(time.Millisecond))

	key := roundtrip(ctx, b, probe)
	if key == "" {
		return
	}
	b.checkPresign(ctx, key, probe)
}

func (b *s3Backend) checkPresign(ctx context.Context, key string, probe *diag.Probe) {
	location, err := b.Locate(ctx, key, 5*time.Minute)
	if err != nil {
		probe.Fail("ссылка игроку", fmt.Sprintf("подписать ссылку не удалось: %v", err), diag.Remedy{
			Hint: "лаунчер получает файлы по временной ссылке; без неё скачать сборку невозможно",
		})
		return
	}
	if location.Kind != LocationURL || location.URL == "" {
		probe.Fail("ссылка игроку", "хранилище не выдало ссылку", diag.Remedy{
			Hint: "проверьте, что ключ доступа умеет подписывать ссылки",
		})
		return
	}
	parsed, err := url.Parse(location.URL)
	if err != nil {
		probe.Fail("ссылка игроку", fmt.Sprintf("ссылка не разбирается: %v", err), diag.Remedy{
			Hint: "проверьте storage.config.endpoint",
		})
		return
	}
	if reason, private := unreachableFromOutside(parsed.Hostname()); private {
		probe.Fail("ссылка игроку", fmt.Sprintf("ссылка ведёт на %s — %s", parsed.Host, reason), diag.Remedy{
			Hint:    "лаунчер получит эту ссылку как есть и не сможет по ней скачать; укажите в storage.config.endpoint адрес, доступный игрокам",
			Command: "laminara-server settings storage.config.endpoint https://s3.example.com",
		})
		return
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location.URL, nil)
	if err != nil {
		probe.Warn("ссылка игроку", fmt.Sprintf("запрос не собрался: %v", err), diag.Remedy{Hint: "повторите проверку"})
		return
	}
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		probe.Fail("ссылка игроку", fmt.Sprintf("по ссылке не скачивается: %v", err), diag.Remedy{
			Hint: "хранилище подписало ссылку, но по ней ничего не отдаётся — проверьте, что адрес из storage.config.endpoint открыт снаружи",
		})
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		probe.Fail("ссылка игроку", fmt.Sprintf("по ссылке приходит %s", response.Status), diag.Remedy{
			Hint: "чаще всего это разошедшееся время на сервере и в хранилище или регион, не совпадающий с бакетом",
		})
		return
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		probe.Fail("ссылка игроку", fmt.Sprintf("скачивание оборвалось: %v", err), diag.Remedy{
			Hint: "проверьте сеть между сервером и хранилищем",
		})
		return
	}
	probe.OK("ссылка игроку", "%s отдаёт файл по временной ссылке", parsed.Host)
}

func unreachableFromOutside(host string) (string, bool) {
	if host == "" {
		return "адрес пуст", true
	}
	if ip := net.ParseIP(host); ip != nil {
		switch {
		case ip.IsLoopback():
			return "это адрес самого сервера", true
		case ip.IsPrivate():
			return "это адрес внутренней сети", true
		case ip.IsLinkLocalUnicast() || ip.IsUnspecified():
			return "это служебный адрес", true
		}
		return "", false
	}
	lowered := strings.ToLower(host)
	if lowered == "localhost" || strings.HasSuffix(lowered, ".local") || strings.HasSuffix(lowered, ".internal") {
		return "это внутреннее имя", true
	}
	if !strings.Contains(lowered, ".") {
		return "это имя контейнера, а не адрес в интернете", true
	}
	return "", false
}
