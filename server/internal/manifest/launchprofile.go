package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const LaunchProfileName = "laminara.profile.json"

type LaunchProfile struct {
	MainClass     string   `json:"mainClass"`
	JavaComponent string   `json:"javaComponent"`
	JavaMajor     int      `json:"javaMajor"`
	OS            string   `json:"os"`
	Arch          string   `json:"arch"`
	PlatformKey   string   `json:"platformKey"`
	JavaBin       string   `json:"javaBin,omitempty"`
	VersionID     string   `json:"versionId"`
	AssetIndex    string   `json:"assetIndex"`
	ClientJar     string   `json:"clientJar"`
	Classpath     []string `json:"classpath"`
	Natives       []string `json:"natives"`
	JvmArgs       []string `json:"jvmArgs,omitempty"`
	GameArgs      []string `json:"gameArgs,omitempty"`
	Runtime       string   `json:"runtime"`
}

func readLaunchProfile(root string) (LaunchProfile, error) {
	var profile LaunchProfile
	path := filepath.Join(root, LaunchProfileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return profile, nil
		}
		return profile, fmt.Errorf("%s не читается: %w", path, err)
	}
	if err := json.Unmarshal(data, &profile); err != nil {
		return profile, fmt.Errorf("%s испорчен — соберите сборку заново командой install: %w", path, err)
	}
	return profile, nil
}
