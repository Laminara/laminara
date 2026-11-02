package daemon

import (
	"errors"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/admin"
	"github.com/laminara/laminara/server/internal/settings"
)

var errExternalSettings = errors.New("конфиг управляется извне; примените изменения в исходном файле, ConfigMap или Secret")

func PreviewSettingsFile(path string, patch *adminv1.SettingsPatch) (*adminv1.PreviewSettingsResponse, error) {
	return (&settingsStore{path: path}).Preview(&adminv1.PreviewSettingsRequest{Patch: patch})
}

func ApplySettingsFile(path string, patch *adminv1.SettingsPatch) (string, error) {
	return (&settingsStore{path: path}).Apply(patch)
}

func (s *settingsStore) Preview(request *adminv1.PreviewSettingsRequest) (*adminv1.PreviewSettingsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, differences, added, err := s.preparePatch(request.Patch, false)
	if err != nil {
		return nil, err
	}
	page, err := s.page(doc, request.Section, request.Entry, request.Query)
	if err != nil {
		return nil, err
	}
	response := &adminv1.PreviewSettingsResponse{Page: admin.SettingsPageProto(page), Differences: differences, AddedPath: added}
	if err := doc.Validate(); err != nil {
		response.ValidationError = err.Error()
	}
	return response, nil
}

func (s *settingsStore) Apply(patch *adminv1.SettingsPatch) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.external {
		return "", errExternalSettings
	}
	if patch == nil || patch.Revision == "" {
		return "", errors.New("перед сохранением загрузите текущую ревизию настроек")
	}
	doc, differences, _, err := s.preparePatch(patch, true)
	if err != nil {
		return "", err
	}
	if len(patch.Changes) == 0 {
		return doc.Revision(), nil
	}
	if err := doc.Save(); err != nil {
		return "", err
	}
	s.pending = true
	if s.changed != nil {
		for _, difference := range differences {
			s.changed(difference.Path)
		}
	}
	return doc.Revision(), nil
}

func (s *settingsStore) preparePatch(patch *adminv1.SettingsPatch, commit bool) (*settings.Doc, []*adminv1.SettingDifference, string, error) {
	if patch != nil && patch.TargetBuild != "" {
		return nil, nil, "", errors.New("это правки сборки; используйте API настроек сборки")
	}
	doc, err := settings.Open(s.path)
	if err != nil {
		return nil, nil, "", err
	}
	differences, added, err := settings.ApplyPatch(doc, patch, commit)
	return doc, differences, added, err
}
