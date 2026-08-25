package manifest

import (
	"encoding/json"
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

func readLaunchProfile(root string) LaunchProfile {
	var profile LaunchProfile
	data, err := os.ReadFile(filepath.Join(root, LaunchProfileName))
	if err != nil {
		return profile
	}
	_ = json.Unmarshal(data, &profile)
	return profile
}
