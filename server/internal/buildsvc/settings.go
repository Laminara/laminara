package buildsvc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/admin"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/settings"
)

func (s *Service) settingsDoc(name string) (*settings.Doc, error) {
	if !safeName(name) {
		return nil, errors.New("недопустимое имя сборки")
	}
	layout := s.layout(name)
	info, err := os.Lstat(layout.root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("сборка должна быть обычным каталогом")
	}
	var sources []manifest.Sources
	if layout.flat {
		sources = append(sources, manifest.Sources{Shared: layout.root, Platform: layout.root})
	}
	for _, platform := range layout.platforms {
		place := layout.place(platform)
		sources = append(sources, manifest.Sources{Shared: place.shared, Platform: place.platform})
	}
	return settings.OpenSchema(filepath.Join(layout.root, manifest.SettingsFileName), "profile", buildSettingsSchema(), func(data []byte) error {
		var value manifest.Settings
		if err := json.Unmarshal(data, &value); err != nil {
			return errors.New("настройки сборки имеют неверные типы")
		}
		return manifest.ValidateSettings(sources, value)
	})
}

func (s *Service) PreviewBuildSettings(ctx context.Context, name string, request *adminv1.PreviewSettingsRequest) (*adminv1.PreviewSettingsResponse, error) {
	if request.Patch != nil && request.Patch.TargetBuild != "" && request.Patch.TargetBuild != name {
		return nil, errors.New("правки предназначены другой сборке")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	release, err := s.alone(name)
	if err != nil {
		return nil, err
	}
	defer release()
	doc, err := s.settingsDoc(name)
	if err != nil {
		return nil, err
	}
	differences, added, err := settings.ApplyPatch(doc, request.Patch, false)
	if err != nil {
		return nil, err
	}
	section := request.Section
	if section == "" && request.Entry == "" {
		section = "profile"
	}
	page, err := admin.DescribeSettings(doc, section, request.Entry, request.Query)
	if err != nil {
		return nil, err
	}
	response := &adminv1.PreviewSettingsResponse{Page: admin.SettingsPageProto(page), Differences: differences, AddedPath: added}
	if err := doc.Validate(); err != nil {
		response.ValidationError = err.Error()
	}
	return response, nil
}

func (s *Service) ApplyBuildSettings(ctx context.Context, name string, patch *adminv1.SettingsPatch) (string, error) {
	if patch != nil && patch.TargetBuild != "" && patch.TargetBuild != name {
		return "", errors.New("правки предназначены другой сборке")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if patch == nil || patch.Revision == "" {
		return "", errors.New("сначала загрузите ревизию настроек сборки")
	}
	release, err := s.alone(name)
	if err != nil {
		return "", err
	}
	defer release()
	doc, err := s.settingsDoc(name)
	if err != nil {
		return "", err
	}
	if _, _, err := settings.ApplyPatch(doc, patch, true); err != nil {
		return "", err
	}
	if err := doc.Save(); err != nil {
		return "", err
	}
	s.fire("build.settings.changed", name)
	return doc.Revision(), nil
}

func buildSettingsSchema() []settings.Section {
	return []settings.Section{{Key: "profile", Title: "Настройки сборки", Fields: []settings.Field{
		{Key: "serverAddress", Label: "Игровой сервер", Kind: settings.KindText, Hint: "Адрес Minecraft-сервера, например play.example.com:25565"},
		{Key: "userWritable", Label: "Файлы игрока", Kind: settings.KindList, Hint: "Пустой список: все файлы сборки проверяются и обновляются лаунчером."},
		{Key: "enforced", Label: "Обязательные файлы", Kind: settings.KindList},
		{Key: "jvmArgs", Label: "Аргументы Java", Kind: settings.KindList},
		{Key: "gameArgs", Label: "Аргументы игры", Kind: settings.KindList},
		{Key: "classpath", Label: "Дополнительный classpath", Kind: settings.KindList},
		{Key: "classpathExclude", Label: "Исключения classpath", Kind: settings.KindList},
		{Key: "mainClass", Label: "Главный класс", Kind: settings.KindText},
	}, Collections: []settings.Collection{featureGroups("features.groups")}}}
}

func featureGroups(key string) settings.Collection {
	return settings.Collection{Key: key, Title: "Группы модов", Hint: "Группа объединяет варианты выбора игрока", Fields: []settings.Field{
		{Key: "title", Label: "Название", Kind: settings.KindText},
		{Key: "id", Label: "Идентификатор", Kind: settings.KindText},
		{Key: "description", Label: "Описание", Kind: settings.KindText},
		{Key: "selection", Label: "Выбор", Kind: settings.KindChoice, Initial: "multi", Options: func() []string { return []string{"single", "multi"} }},
		{Key: "required", Label: "Выбор обязателен", Kind: settings.KindBool, Default: "false"},
	}, Children: func() []settings.Collection { return []settings.Collection{featureOptions()} }}
}

func featureOptions() settings.Collection {
	return settings.Collection{Key: "options", Title: "Варианты модов", Fields: []settings.Field{
		{Key: "title", Label: "Название", Kind: settings.KindText},
		{Key: "id", Label: "Идентификатор", Kind: settings.KindText},
		{Key: "description", Label: "Описание", Kind: settings.KindText},
		{Key: "defaultEnabled", Label: "Включён по умолчанию", Kind: settings.KindBool, Default: "false"},
		{Key: "files", Label: "Файлы мода", Kind: settings.KindList},
		{Key: "meta.requires", Label: "Зависимости", Kind: settings.KindList, Hint: "Адреса вариантов: группа#вариант"},
		{Key: "meta.incompatibleWith", Label: "Несовместим с", Kind: settings.KindList},
		{Key: "meta.icon", Label: "Иконка", Kind: settings.KindText},
		{Key: "meta.badge", Label: "Метка", Kind: settings.KindText},
		{Key: "jvmArgs", Label: "Аргументы Java", Kind: settings.KindList},
		{Key: "gameArgs", Label: "Аргументы игры", Kind: settings.KindList},
		{Key: "classpath", Label: "Classpath", Kind: settings.KindList},
	}, Children: func() []settings.Collection { return []settings.Collection{featureGroups("groups")} }}
}
