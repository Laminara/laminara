package prepare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/laminara/laminara/server/internal/atomicfile"
	"github.com/laminara/laminara/server/internal/authlib"
	"github.com/laminara/laminara/server/internal/compat"
	"github.com/laminara/laminara/server/internal/httpx"
	"github.com/laminara/laminara/server/internal/jre"
	"github.com/laminara/laminara/server/internal/loader"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/mojang"
	"github.com/laminara/laminara/server/internal/platform"
	"github.com/laminara/laminara/server/internal/progress"
	"github.com/laminara/laminara/server/internal/resolve"
)

const defaultWorkers = 12

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
		http:          httpx.NewClient(5 * time.Minute),
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
	ProfileDir       string
	PlatformDir      string
	VersionURL       string
	OS               string
	Arch             string
	PlatformKey      string
	MinecraftVersion string
	LoaderName       string
	LoaderVersion    string
	LoaderInManifest bool
	JavaComponent    string
	Files            []compat.File
}

func (p *Preparer) Prepare(ctx context.Context, opts Options) (*resolve.Profile, error) {
	if opts.PlatformDir == "" {
		opts.PlatformDir = opts.ProfileDir
	}
	progress.Phase(ctx, "Метаданные версии")
	detail, err := p.mojang.FetchVersion(ctx, opts.VersionURL)
	if err != nil {
		return nil, err
	}

	var loaderProfile *loader.LoaderProfile
	var loaderInstaller loader.Installer
	loaderJava := ""
	if opts.LoaderName != "" && opts.LoaderName != "vanilla" && !opts.LoaderInManifest {
		selected, ok := loader.Get(opts.LoaderName)
		if !ok {
			return nil, fmt.Errorf("загрузчика «%s» нет — какие есть для версии, покажет loaders <версия>", opts.LoaderName)
		}
		if raiser, ok := selected.(loader.JavaProvider); ok {
			loaderJava = raiser.JavaComponent()
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
	javaMajor := profile.JavaMajor
	for _, override := range []string{loaderJava, opts.JavaComponent} {
		if override == "" {
			continue
		}
		javaComponent = override
		if major, known := componentMajors[override]; known {
			javaMajor = major
		}
	}

	dl := &downloader{http: p.http, root: opts.ProfileDir, workers: p.workers}
	jobs := []job{{url: profile.ClientJar.URL, path: profile.ClientJar.Path, digest: sha1Digest(profile.ClientJar.SHA1)}}
	for _, lib := range profile.Libraries {
		jobs = append(jobs, job{url: lib.URL, path: lib.Path, digest: sha1Digest(lib.SHA1)})
	}
	for _, native := range profile.Natives {
		jobs = append(jobs, job{url: native.URL, path: native.Path, digest: sha1Digest(native.SHA1)})
	}
	if err := dl.run(ctx, jobs, "Клиент и библиотеки"); err != nil {
		return nil, err
	}
	if err := p.downloadAssets(ctx, opts.ProfileDir, profile.AssetIndexID, profile.AssetIndexURL); err != nil {
		return nil, err
	}
	javaBin, err := p.downloadRuntime(ctx, opts.PlatformDir, opts.PlatformKey, javaComponent)
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

	if err := p.downloadExtraFiles(ctx, opts); err != nil {
		return nil, err
	}
	if err := p.writeLaunchProfile(opts, profile, javaComponent, javaMajor, javaBin, detail.ID, installResult); err != nil {
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
	if err := p.ensureAuthlib(ctx, opts.ProfileDir); err != nil {
		return nil, err
	}
	return profile, nil
}

func (p *Preparer) downloadExtraFiles(ctx context.Context, opts Options) error {
	if len(opts.Files) == 0 {
		return nil
	}
	dl := &downloader{http: p.http, root: opts.ProfileDir, workers: p.workers}
	jobs := make([]job, 0, len(opts.Files))
	for _, file := range opts.Files {
		jobs = append(jobs, job{url: file.URL, path: file.Path, digest: sha256Digest(file.SHA256)})
	}
	return dl.run(ctx, jobs, "Моды рецепта")
}

func (p *Preparer) ensureAuthlib(ctx context.Context, dir string) error {
	progress.Phase(ctx, "Вход в игре")
	version, err := authlib.Ensure(ctx, p.http, dir, func(ctx context.Context, url, dest, sha1 string) error {
		return downloadFile(ctx, p.http, url, dest, sha1Digest(sha1), false)
	})
	if err != nil {
		return fmt.Errorf("не удалось получить %s, без него игрок не войдёт на сервер: %w", authlib.FileName, err)
	}
	if version != "" {
		slog.Info("authlib-injector добавлен в сборку", "source", "build", "версия", version)
	}
	return nil
}

func (p *Preparer) runInstaller(ctx context.Context, opts Options, mcVersion, javaComponent, clientJarPath string) (*loader.InstallResult, error) {
	javaBin, err := p.serverJavaBin(ctx, opts.PlatformDir, javaComponent)
	if err != nil {
		return nil, err
	}
	installer, err := loaderMustInstaller(opts.LoaderName)
	if err != nil {
		return nil, err
	}
	return installer.Install(ctx, loader.InstallRequest{
		MCVersion:     mcVersion,
		LoaderVersion: opts.LoaderVersion,
		ProfileDir:    opts.ProfileDir,
		LibrariesDir:  filepath.Join(opts.ProfileDir, "libraries"),
		MinecraftJar:  filepath.Join(opts.ProfileDir, filepath.FromSlash(clientJarPath)),
		JavaBin:       javaBin,
		Download: func(ctx context.Context, url, dest, sha1 string) error {
			return downloadFile(ctx, p.http, url, dest, sha1Digest(sha1), false)
		},
		Fetch: func(ctx context.Context, url string) ([]byte, error) {
			return fetchSmall(ctx, p.http, url)
		},
	})
}

func loaderMustInstaller(name string) (loader.Installer, error) {
	selected, ok := loader.Get(name)
	if !ok {
		return nil, fmt.Errorf("загрузчика «%s» нет", name)
	}
	installer, ok := selected.(loader.Installer)
	if !ok {
		return nil, fmt.Errorf("загрузчик «%s» не умеет ставить себя установщиком", name)
	}
	return installer, nil
}

func fetchSmall(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s ответил %s", url, response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 1<<10))
}

func (p *Preparer) serverJavaBin(ctx context.Context, root, component string) (string, error) {
	platformKey, _ := platform.Key(platform.FromRuntime(runtime.GOOS, runtime.GOARCH))
	selected, err := p.jre.Select(ctx, platformKey, component)
	if err != nil {
		return "", noRuntimeHint(err, platformKey, "установщику загрузчика не на чем работать")
	}
	files, err := p.jre.FetchFiles(ctx, selected.Manifest.URL)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, ".laminara", "buildjre", platformKey)
	dl := &downloader{http: p.http, root: dir, workers: p.workers}
	jobs := make([]job, 0, len(files))
	for _, file := range files {
		jobs = append(jobs, job{url: file.Download.URL, path: file.Path, digest: sha1Digest(file.Download.SHA1), executable: file.Executable})
	}
	if err := dl.run(ctx, jobs, "Java для сборки"); err != nil {
		return "", err
	}
	return filepath.Join(dir, "bin", "java"), nil
}

func movedToAnotherJava(platformDir, component string) bool {
	previous, err := manifest.ReadLaunchProfile(platformDir)
	return err == nil && previous.JavaComponent != "" && previous.JavaComponent != component
}

func (p *Preparer) downloadRuntime(ctx context.Context, root, platformKey, component string) (string, error) {
	runtimeInfo, err := p.jre.Select(ctx, platformKey, component)
	if err != nil {
		return "", noRuntimeHint(err, platformKey, "положите свой JDK в папку сборки и укажите его в javaBin файла "+manifest.LaunchProfileName)
	}
	files, err := p.jre.FetchFiles(ctx, runtimeInfo.Manifest.URL)
	if err != nil {
		return "", err
	}

	inside := "runtime/" + platformKey
	if movedToAnotherJava(root, component) {
		inside = "runtime/." + platformKey + ".new"
		if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(inside))); err != nil {
			return "", err
		}
	}

	dl := &downloader{http: p.http, root: root, workers: p.workers}
	jobs := make([]job, 0, len(files))
	for _, file := range files {
		jobs = append(jobs, job{
			url:        file.Download.URL,
			path:       inside + "/" + file.Path,
			digest:     sha1Digest(file.Download.SHA1),
			executable: file.Executable,
		})
	}
	if err := dl.run(ctx, jobs, "Java рантайм"); err != nil {
		return "", err
	}
	if inside != "runtime/"+platformKey {
		if err := swapRuntime(root, inside, "runtime/"+platformKey, component); err != nil {
			return "", err
		}
	}
	javaBin, err := resolveJavaBin(files, platformKey)
	if err != nil {
		return "", err
	}
	return javaBin, nil
}

func swapRuntime(root, fresh, live, component string) error {
	freshDir := filepath.Join(root, filepath.FromSlash(fresh))
	liveDir := filepath.Join(root, filepath.FromSlash(live))
	slog.Info("сборка переезжает на другую Java, старый рантайм заменяю", "стало", component, "папка", liveDir)
	if err := os.RemoveAll(liveDir); err != nil {
		return err
	}
	return os.Rename(freshDir, liveDir)
}

func noRuntimeHint(err error, platformKey, remedy string) error {
	if !errors.Is(err, jre.ErrNoMojangRuntime) {
		return err
	}
	return fmt.Errorf("у Mojang нет готовой Java для платформы %s — %s", platformKey, remedy)
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
	return "", fmt.Errorf("в скачанной Java для %s нет исполняемого файла — удалите её и соберите заново", platformKey)
}

func (p *Preparer) writeLaunchProfile(opts Options, profile *resolve.Profile, javaComponent string, javaMajor int, javaBin, versionID string, install *loader.InstallResult) error {
	classpath := make([]string, 0, len(profile.Libraries)+1)
	for _, lib := range profile.Libraries {
		classpath = append(classpath, lib.Path)
	}

	mainClass := profile.MainClass
	clientJar := profile.ClientJar.Path
	jvmArgs := profile.JvmArgs
	gameArgs := profile.GameArgs
	var onDisk []string
	if install != nil {
		onDisk = install.OnDisk
		mainClass = install.MainClass
		clientJar = install.ClientJar
		jvmArgs = append(jvmArgs, install.JVMArgs...)
		gameArgs = append(gameArgs, install.GameArgs...)
		classpath = mergeUnique(classpath, install.Libraries)
	}
	classpath = append(classpath, clientJar)

	natives := make([]string, 0, len(profile.Natives))
	for _, native := range profile.Natives {
		natives = append(natives, native.Path)
	}

	launch := manifest.LaunchProfile{
		MainClass:        mainClass,
		JavaComponent:    javaComponent,
		JavaMajor:        javaMajor,
		OS:               opts.OS,
		Arch:             opts.Arch,
		PlatformKey:      opts.PlatformKey,
		JavaBin:          javaBin,
		VersionID:        versionID,
		MinecraftVersion: opts.MinecraftVersion,
		AssetIndex:       profile.AssetIndexID,
		ClientJar:        clientJar,
		Classpath:        classpath,
		ExtraLibraries:   onDisk,
		Natives:          natives,
		JvmArgs:          jvmArgs,
		GameArgs:         gameArgs,
		Runtime:          "runtime/" + opts.PlatformKey,
	}
	data, err := json.MarshalIndent(launch, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(opts.PlatformDir, 0o750); err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(opts.PlatformDir, manifest.LaunchProfileName), data, 0o644)
}

func mergeUnique(base, extra []string) []string {
	at := make(map[string]int, len(base))
	for i, path := range base {
		at[artifactOf(path)] = i
	}
	for _, path := range extra {
		artifact := artifactOf(path)
		if index, found := at[artifact]; found {
			base[index] = path
			continue
		}
		at[artifact] = len(base)
		base = append(base, path)
	}
	return base
}

func artifactOf(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) < 3 {
		return filepath.ToSlash(path)
	}
	return strings.Join(parts[:len(parts)-2], "/")
}

var componentMajors = map[string]int{
	"jre-legacy":                  8,
	"java-runtime-alpha":          16,
	"java-runtime-beta":           17,
	"java-runtime-gamma":          17,
	"java-runtime-gamma-snapshot": 17,
	"java-runtime-delta":          21,
	"java-runtime-epsilon":        25,
}
