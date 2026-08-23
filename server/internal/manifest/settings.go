package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const SettingsFileName = "laminara.settings.json"

var DefaultUserWritable = []string{
	"options.txt",
	"optionsof.txt",
	"optionsshaders.txt",
	"servers.dat",
	"servers.dat_old",
	"usercache.json",
	"config/",
	"saves/",
	"screenshots/",
	"logs/",
	"crash-reports/",
	"resourcepacks/",
	"shaderpacks/",
	"schematics/",
	"backups/",
	"journeymap/",
	".fabric/",
	".bobby/",
}

type Settings struct {
	UserWritable  []string     `json:"userWritable"`
	Enforced      []string     `json:"enforced"`
	ServerAddress string       `json:"serverAddress"`
	Loader        string       `json:"loader"`
	Features      *FeatureSpec `json:"features,omitempty"`
}

type FeatureSpec struct {
	Groups []GroupSpec `json:"groups"`
}

type GroupSpec struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Selection   string       `json:"selection"`
	Required    bool         `json:"required,omitempty"`
	Options     []OptionSpec `json:"options"`
}

type OptionSpec struct {
	ID             string      `json:"id"`
	Title          string      `json:"title"`
	Description    string      `json:"description,omitempty"`
	DefaultEnabled bool        `json:"defaultEnabled,omitempty"`
	Files          []string    `json:"files,omitempty"`
	Groups         []GroupSpec `json:"groups,omitempty"`
	Meta           *MetaSpec   `json:"meta,omitempty"`
}

type MetaSpec struct {
	Icon             string   `json:"icon,omitempty"`
	Badge            string   `json:"badge,omitempty"`
	Requires         []string `json:"requires,omitempty"`
	IncompatibleWith []string `json:"incompatibleWith,omitempty"`
}

func SetLoader(root, loader string) error {
	settings, err := LoadSettings(root)
	if err != nil {
		return err
	}
	settings.Loader = loader
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, SettingsFileName), data, 0o644)
}

func LoadSettings(root string) (Settings, error) {
	data, err := os.ReadFile(filepath.Join(root, SettingsFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return Settings{}, nil
		}
		return Settings{}, err
	}
	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

func EnsureDefaultSettings(root string) error {
	path := filepath.Join(root, SettingsFileName)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	settings := Settings{UserWritable: DefaultUserWritable, Enforced: []string{}, ServerAddress: ""}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
