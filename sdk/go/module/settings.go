package module

type SettingKind string

const (
	SettingText       SettingKind = "text"
	SettingSecret     SettingKind = "secret"
	SettingBool       SettingKind = "bool"
	SettingInt        SettingKind = "int"
	SettingDuration   SettingKind = "duration"
	SettingChoice     SettingKind = "choice"
	SettingList       SettingKind = "list"
	SettingPairs      SettingKind = "pairs"
	SettingMap        SettingKind = "map"
	SettingJSON       SettingKind = "json"
	SettingSecretJSON SettingKind = "secret-json"
)

type SettingField struct {
	Key       string      `json:"key"`
	Label     string      `json:"label"`
	Hint      string      `json:"hint,omitempty"`
	Group     string      `json:"group,omitempty"`
	Kind      SettingKind `json:"kind"`
	Default   string      `json:"default,omitempty"`
	Options   []string    `json:"options,omitempty"`
	Required  bool        `json:"required,omitempty"`
	AllowZero bool        `json:"allowZero,omitempty"`
	Min       *int64      `json:"min,omitempty"`
	Max       *int64      `json:"max,omitempty"`
}

type SettingsDescriptor struct {
	Version   int                       `json:"version,omitempty"`
	Module    []SettingField            `json:"module,omitempty"`
	Providers map[string][]SettingField `json:"providers,omitempty"`
}

type SettingsDescriber interface {
	Settings() SettingsDescriptor
}
