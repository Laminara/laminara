package mojang

type Artifact struct {
	Path string `json:"path"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

type LibraryDownloads struct {
	Artifact    *Artifact            `json:"artifact"`
	Classifiers map[string]*Artifact `json:"classifiers"`
}

type OSRule struct {
	Name    string `json:"name"`
	Arch    string `json:"arch"`
	Version string `json:"version"`
}

type Rule struct {
	Action   string          `json:"action"`
	OS       *OSRule         `json:"os"`
	Features map[string]bool `json:"features"`
}

type Library struct {
	Name      string            `json:"name"`
	Downloads LibraryDownloads  `json:"downloads"`
	Rules     []Rule            `json:"rules"`
	Natives   map[string]string `json:"natives"`
	URL       string            `json:"url"`
}
