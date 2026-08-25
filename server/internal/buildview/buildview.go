package buildview

import (
	"strconv"
	"strings"
	"time"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/humanize"
	"github.com/laminara/laminara/server/internal/platform"
)

type Field struct {
	Label string
	Value string
	Hint  string
}

func StatusWord(status string) string {
	switch status {
	case "published":
		return "опубликована"
	case "prepared":
		return "собрана, ждёт публикации"
	default:
		return status
	}
}

func LoaderWord(loader string) string {
	if loader == "" {
		return "vanilla"
	}
	return loader
}

func PlayersWord(players *adminv1.BuildPlayers) (string, string) {
	if players == nil {
		return "", ""
	}
	if players.Address == "" {
		return "адрес сервера не указан", ""
	}
	if !players.Reachable {
		return "сервер не отвечает", players.Address
	}
	return humanize.Count(int(players.Online), "игрок", "игрока", "игроков") + " из " + strconv.FormatInt(players.Max, 10), players.Address
}

const (
	AddressHint = "serverAddress в настройках сборки"
	AddressNote = "Адрес сервера берётся из serverAddress в настройках сборки."
)

func Title(build *adminv1.BuildInfo) string {
	return "Сборка «" + build.Name + "» — " + StatusWord(build.Status)
}

func Fields(build *adminv1.BuildInfo, players *adminv1.BuildPlayers) []Field {
	fields := []Field{}
	if build.MinecraftVersion != "" {
		fields = append(fields, Field{Label: "Minecraft", Value: build.MinecraftVersion})
		fields = append(fields, Field{Label: "Загрузчик", Value: LoaderWord(build.Loader)})
	}
	if build.JavaMajor > 0 {
		fields = append(fields, Field{Label: "Java", Value: strconv.FormatUint(uint64(build.JavaMajor), 10)})
	}
	if build.SizeBytes > 0 {
		size := humanize.Bytes(build.SizeBytes)
		if build.Files > 0 {
			size += ", " + humanize.Count(int(build.Files), "файл", "файла", "файлов")
		}
		fields = append(fields, Field{Label: "Размер", Value: size})
	}
	if value := platformWord(build.Published); value != "" {
		fields = append(fields, Field{Label: "Платформы", Value: value})
	} else if value := platformWord(build.Prepared); value != "" {
		fields = append(fields, Field{Label: "Платформы", Value: value, Hint: "собрана, но ещё не опубликована"})
	}
	if build.PublishedAtUnixNanos > 0 {
		fields = append(fields, Field{Label: "Опубликована", Value: humanize.When(time.Unix(0, build.PublishedAtUnixNanos))})
	}
	if value, hint := PlayersWord(players); value != "" {
		fields = append(fields, Field{Label: "Игроки", Value: value, Hint: hint})
	} else if build.ServerAddress != "" {
		fields = append(fields, Field{Label: "Адрес сервера", Value: build.ServerAddress})
	}
	if build.Access != "" {
		fields = append(fields, Field{Label: "Доступ", Value: build.Access})
	}
	if build.HasFeatures {
		fields = append(fields, Field{Label: "Моды по выбору", Value: "есть"})
	}
	return fields
}

func platformWord(list []corev1.Platform) string {
	if len(list) == 0 {
		return ""
	}
	return strings.Join(platform.Keys(list), ", ")
}
