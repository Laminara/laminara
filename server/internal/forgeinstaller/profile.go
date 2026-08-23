package forgeinstaller

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type sidePair struct {
	Client string `json:"client"`
	Server string `json:"server"`
}

type artifact struct {
	Path string `json:"path"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

type Library struct {
	Name      string `json:"name"`
	Downloads struct {
		Artifact artifact `json:"artifact"`
	} `json:"downloads"`
}

type Processor struct {
	Sides     []string          `json:"sides"`
	Jar       string            `json:"jar"`
	Classpath []string          `json:"classpath"`
	Args      []string          `json:"args"`
	Outputs   map[string]string `json:"outputs"`
}

type installProfile struct {
	Spec       int                 `json:"spec"`
	Minecraft  string              `json:"minecraft"`
	Version    string              `json:"version"`
	JSON       string              `json:"json"`
	Data       map[string]sidePair `json:"data"`
	Processors []Processor         `json:"processors"`
	Libraries  []Library           `json:"libraries"`
}

type versionProfile struct {
	ID           string `json:"id"`
	InheritsFrom string `json:"inheritsFrom"`
	MainClass    string `json:"mainClass"`
	Arguments    struct {
		JVM  []string `json:"jvm"`
		Game []string `json:"game"`
	} `json:"arguments"`
	Libraries []Library `json:"libraries"`
}

type Installer struct {
	jarPath string
	profile installProfile
	version versionProfile
}

func Open(jarPath string) (*Installer, error) {
	reader, err := zip.OpenReader(jarPath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var install Installer
	install.jarPath = jarPath
	if err := readZipJSON(&reader.Reader, "install_profile.json", &install.profile); err != nil {
		return nil, err
	}
	versionPath := strings.TrimPrefix(install.profile.JSON, "/")
	if versionPath == "" {
		versionPath = "version.json"
	}
	if err := readZipJSON(&reader.Reader, versionPath, &install.version); err != nil {
		return nil, err
	}
	return &install, nil
}

func (i *Installer) MinecraftVersion() string { return i.profile.Minecraft }
func (i *Installer) MainClass() string        { return i.version.MainClass }
func (i *Installer) InheritsFrom() string     { return i.version.InheritsFrom }

func (i *Installer) Libraries() []Library {
	libraries := make([]Library, 0, len(i.profile.Libraries)+len(i.version.Libraries))
	libraries = append(libraries, i.profile.Libraries...)
	libraries = append(libraries, i.version.Libraries...)
	return libraries
}

func readZipJSON(reader *zip.Reader, name string, target any) error {
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, target)
	}
	return fmt.Errorf("forgeinstaller: %s not found in installer", name)
}
