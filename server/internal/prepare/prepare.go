package prepare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/laminara/laminara/server/internal/jre"
	"github.com/laminara/laminara/server/internal/loader"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/mojang"
	"github.com/laminara/laminara/server/internal/progress"
	"github.com/laminara/laminara/server/internal/resolve"
)

const defaultWorkers = 12

const LaunchProfileName = "laminara.profile.json"

type Preparer struct {
	mojang        *mojang.Client
	jre           *jre.Client
	http          *http.Client
	assetsBaseURL string
	workers       int
}

func NewPreparer() *Preparer {
	return &Preparer{
		mojang:        mojang.NewClient(),
		jre:           jre.NewClient(),
		http:          &http.Client{Timeout: 5 * time.Minute},
		assetsBaseURL: defaultAssetsBaseURL,
		workers:       defaultWorkers,
	}
}

func NewPreparerWith(httpClient *http.Client, assetsBaseURL, jreAllURL string, workers int) *Preparer {
	return &Preparer{
		mojang:        mojang.NewClientWith(httpClient, ""),
		jre:           jre.NewClientWith(httpClient, jreAllURL),
		http:          httpClient,
		assetsBaseURL: assetsBaseURL,
		workers:       workers,
	}
}

type Options struct {
	ProfileDir    string
	VersionURL    string
	OS            string
	Arch          string
	PlatformKey   string
	LoaderName    string
	LoaderVersion string
	JavaComponent string
}

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

func (p *Preparer) Prepare(ctx context.Context, opts Options) (*resolve.Profile, error) {
	progress.Phase(ctx, "Метаданные версии")
	detail, err := p.mojang.FetchVersion(ctx, opts.VersionURL)
	if err != nil {
		return nil, err
	}

	var loaderProfile *loader.LoaderProfile
	var loaderInstaller loader.Installer
	if opts.LoaderName != "" && opts.LoaderName != "vanilla" {
		selected, ok := loader.Get(opts.LoaderName)
		if !ok {
			return nil, fmt.Errorf("unknown loader %q", opts.LoaderName)
		}
		if installer, ok := selected.(loader.Installer); ok {
			loaderInstaller = installer
		} else {
			loaderProfile, err = selected.Resolve(ctx, detail.ID, opts.LoaderVersion)
			if err != nil {
				return nil, err
			}
		}
	}

	profile, err := resolve.Resolve(detail, opts.OS, opts.Arch, loaderProfile)
	if err != nil {
		return nil, err
	}
	javaComponent := profile.JavaComponent
	if opts.JavaComponent != "" {
		javaComponent = opts.JavaComponent
	}

	dl := &downloader{http: p.http, root: opts.ProfileDir, workers: p.workers}
	jobs := []job{{url: profile.ClientJar.URL, path: profile.ClientJar.Path, sha1: profile.ClientJar.SHA1}}
	for _, lib := range profile.Libraries {
		jobs = append(jobs, job{url: lib.URL, path: lib.Path, sha1: lib.SHA1})
	}
	for _, native := range profile.Natives {
		jobs = append(jobs, job{url: native.URL, path: native.Path, sha1: native.SHA1})
	}
	if err := dl.run(ctx, jobs, "Клиент и библиотеки"); err != nil {
		return nil, err
	}
	if err := p.downloadAssets(ctx, opts.ProfileDir, profile.AssetIndexID, profile.AssetIndexURL); err != nil {
		return nil, err
	}
	javaBin, err := p.downloadRuntime(ctx, opts.ProfileDir, opts.PlatformKey, javaComponent)
	if err != nil {
		return nil, err
	}

	var installResult *loader.InstallResult
	if loaderInstaller != nil {
		progress.Phase(ctx, "Установка загрузчика")
		installResult, err = p.runInstaller(ctx, opts, detail.ID, javaComponent, profile.ClientJar.Path)
		if err != nil {
			return nil, err
		}
	}

	if err := p.writeLaunchProfile(opts, profile, javaComponent, javaBin, detail.ID, installResult); err != nil {
		return nil, err
	}
	if err := manifest.EnsureDefaultSettings(opts.ProfileDir); err != nil {
		return nil, err
	}
	loaderName := opts.LoaderName
	if loaderName == "" {
		loaderName = "vanilla"
	}
	if err := manifest.SetLoader(opts.ProfileDir, loaderName); err != nil {
		return nil, err
	}
	return profile, nil
}

func (p *Preparer) runInstaller(ctx context.Context, opts Options, mcVersion, javaComponent, clientJarPath string) (*loader.InstallResult, error) {
	javaBin, err := p.serverJavaBin(ctx, opts.ProfileDir, javaComponent)
	if err != nil {
		return nil, err
	}
	installer := loaderMustInstaller(opts.LoaderName)
	return installer.Install(ctx, loader.InstallRequest{
		MCVersion:     mcVersion,
		LoaderVersion: opts.LoaderVersion,
		ProfileDir:    opts.ProfileDir,
		LibrariesDir:  filepath.Join(opts.ProfileDir, "libraries"),
		MinecraftJar:  filepath.Join(opts.ProfileDir, filepath.FromSlash(clientJarPath)),
		JavaBin:       javaBin,
		Download: func(ctx context.Context, url, dest, sha1 string) error {
			return downloadFile(ctx, p.http, url, dest, sha1, false)
		},
	})
}

func loaderMustInstaller(name string) loader.Installer {
	selected, _ := loader.Get(name)
	return selected.(loader.Installer)
}

func (p *Preparer) serverJavaBin(ctx context.Context, root, component string) (string, error) {
	platformKey := jre.PlatformKey(runtime.GOOS, runtime.GOARCH)
	selected, err := p.jre.Select(ctx, platformKey, component)
	if err != nil {
		return "", err
	}
	files, err := p.jre.FetchFiles(ctx, selected.Manifest.URL)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, ".laminara", "buildjre", platformKey)
	dl := &downloader{http: p.http, root: dir, workers: p.workers}
	jobs := make([]job, 0, len(files))
	for _, file := range files {
		jobs = append(jobs, job{url: file.Download.URL, path: file.Path, sha1: file.Download.SHA1, executable: file.Executable})
	}
	if err := dl.run(ctx, jobs, "Java для сборки"); err != nil {
		return "", err
	}
	return filepath.Join(dir, "bin", "java"), nil
}

func (p *Preparer) downloadRuntime(ctx context.Context, root, platformKey, component string) (string, error) {
	runtimeInfo, err := p.jre.Select(ctx, platformKey, component)
	if err != nil {
		return "", err
	}
	files, err := p.jre.FetchFiles(ctx, runtimeInfo.Manifest.URL)
	if err != nil {
		return "", err
	}
	dl := &downloader{http: p.http, root: root, workers: p.workers}
	jobs := make([]job, 0, len(files))
	for _, file := range files {
		jobs = append(jobs, job{
			url:        file.Download.URL,
			path:       "runtime/" + platformKey + "/" + file.Path,
			sha1:       file.Download.SHA1,
			executable: file.Executable,
		})
	}
	if err := dl.run(ctx, jobs, "Java рантайм"); err != nil {
		return "", err
	}
	javaBin, err := resolveJavaBin(files, platformKey)
	if err != nil {
		return "", err
	}
	return javaBin, nil
}

func resolveJavaBin(files []jre.RuntimeFile, platformKey string) (string, error) {
	candidates := []string{"bin/javaw.exe", "bin/java.exe", "bin/java"}
	for _, candidate := range candidates {
		for _, file := range files {
			path := filepath.ToSlash(file.Path)
			if path == candidate || strings.HasSuffix(path, "/"+candidate) {
				return "runtime/" + platformKey + "/" + path, nil
			}
		}
	}
	return "", fmt.Errorf("no java executable in the %s runtime", platformKey)
}

func (p *Preparer) writeLaunchProfile(opts Options, profile *resolve.Profile, javaComponent, javaBin, versionID string, install *loader.InstallResult) error {
	classpath := make([]string, 0, len(profile.Libraries)+1)
	for _, lib := range profile.Libraries {
		classpath = append(classpath, lib.Path)
	}

	mainClass := profile.MainClass
	clientJar := profile.ClientJar.Path
	var jvmArgs, gameArgs []string
	if install != nil {
		mainClass = install.MainClass
		clientJar = install.ClientJar
		jvmArgs = install.JVMArgs
		gameArgs = install.GameArgs
		classpath = mergeUnique(classpath, install.Libraries)
	}
	classpath = append(classpath, clientJar)

	natives := make([]string, 0, len(profile.Natives))
	for _, native := range profile.Natives {
		natives = append(natives, native.Path)
	}

	launch := LaunchProfile{
		MainClass:     mainClass,
		JavaComponent: javaComponent,
		JavaMajor:     profile.JavaMajor,
		OS:            opts.OS,
		Arch:          opts.Arch,
		PlatformKey:   opts.PlatformKey,
		JavaBin:       javaBin,
		VersionID:     versionID,
		AssetIndex:    profile.AssetIndexID,
		ClientJar:     clientJar,
		Classpath:     classpath,
		Natives:       natives,
		JvmArgs:       jvmArgs,
		GameArgs:      gameArgs,
		Runtime:       "runtime/" + opts.PlatformKey,
	}
	data, err := json.MarshalIndent(launch, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(opts.ProfileDir, LaunchProfileName), data, 0o644)
}

func mergeUnique(base, extra []string) []string {
	seen := make(map[string]bool, len(base))
	for _, path := range base {
		seen[path] = true
	}
	for _, path := range extra {
		if !seen[path] {
			seen[path] = true
			base = append(base, path)
		}
	}
	return base
}
