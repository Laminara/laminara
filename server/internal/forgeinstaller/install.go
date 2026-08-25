package forgeinstaller

import (
	"archive/zip"
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/laminara/laminara/server/internal/progress"
)

type Downloader func(ctx context.Context, url, dest, sha1 string) error

type Request struct {
	LibrariesDir string
	ProfileDir   string
	MinecraftJar string
	JavaBin      string
	DataDir      string
	Download     Downloader
}

type LaunchInfo struct {
	MainClass string
	JVMArgs   []string
	GameArgs  []string
	Libraries []string
	ClientJar string
}

func (i *Installer) Install(ctx context.Context, req Request) (*LaunchInfo, error) {
	if i.legacy != nil {
		return i.legacyInstall(ctx, req)
	}
	if err := i.extractEmbeddedMaven(req.LibrariesDir); err != nil {
		return nil, err
	}
	progress.Phase(ctx, "Библиотеки загрузчика")
	if err := i.downloadLibraries(ctx, req); err != nil {
		return nil, err
	}
	placeholders, err := i.placeholders(req)
	if err != nil {
		return nil, err
	}

	clientProcessors := int64(0)
	for _, processor := range i.profile.Processors {
		if includesClient(processor.Sides) {
			clientProcessors++
		}
	}
	completed := int64(0)
	for _, processor := range i.profile.Processors {
		if !includesClient(processor.Sides) {
			continue
		}
		completed++
		progress.Report(ctx, progress.Event{Phase: "Патч клиента", Current: completed, Total: clientProcessors, Message: processorName(processor.Jar)})
		command, err := i.processorCommand(processor, placeholders, req)
		if err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, command[0], command[1:]...)
		cmd.Dir = req.ProfileDir
		if output, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("processor %s failed: %w\n%s", processor.Jar, err, output)
		}
	}

	launch := &LaunchInfo{
		MainClass: i.version.MainClass,
		JVMArgs:   i.version.Arguments.JVM,
		GameArgs:  i.version.Arguments.Game,
	}
	if len(launch.GameArgs) == 0 {
		launch.GameArgs = tweakArgs(i.version.MinecraftArguments)
	}
	for _, library := range i.Libraries() {
		path, err := libraryPath(library)
		if err != nil {
			return nil, err
		}
		launch.Libraries = append(launch.Libraries, filepath.ToSlash(path))
	}
	if patched, ok := placeholders["PATCHED"]; ok {
		if relative, err := filepath.Rel(req.LibrariesDir, patched); err == nil {
			launch.ClientJar = filepath.ToSlash(relative)
		}
	}
	return launch, nil
}

func (i *Installer) downloadLibraries(ctx context.Context, req Request) error {
	for _, library := range i.Libraries() {
		if library.Downloads.Artifact.URL == "" {
			continue
		}
		path, err := libraryPath(library)
		if err != nil {
			return err
		}
		dest := filepath.Join(req.LibrariesDir, filepath.FromSlash(path))
		if err := req.Download(ctx, library.Downloads.Artifact.URL, dest, library.Downloads.Artifact.SHA1); err != nil {
			return err
		}
	}
	return nil
}

func (i *Installer) placeholders(req Request) (map[string]string, error) {
	placeholders := map[string]string{
		"SIDE":          "client",
		"MINECRAFT_JAR": req.MinecraftJar,
		"ROOT":          req.ProfileDir,
		"INSTALLER":     i.jarPath,
	}
	for key, pair := range i.profile.Data {
		resolved, err := i.resolveDataValue(pair.Client, req)
		if err != nil {
			return nil, err
		}
		placeholders[key] = resolved
	}
	return placeholders, nil
}

func (i *Installer) resolveDataValue(value string, req Request) (string, error) {
	if strings.HasPrefix(value, "/") {
		return i.extractFile(value, req.DataDir)
	}
	return resolveToken(value, nil, req.LibrariesDir)
}

func (i *Installer) processorCommand(processor Processor, placeholders map[string]string, req Request) ([]string, error) {
	jarRelative, err := mavenPath(processor.Jar)
	if err != nil {
		return nil, err
	}
	jarPath := filepath.Join(req.LibrariesDir, filepath.FromSlash(jarRelative))
	mainClass, err := jarMainClass(jarPath)
	if err != nil {
		return nil, err
	}
	classpath := []string{jarPath}
	for _, entry := range processor.Classpath {
		relative, err := mavenPath(entry)
		if err != nil {
			return nil, err
		}
		classpath = append(classpath, filepath.Join(req.LibrariesDir, filepath.FromSlash(relative)))
	}
	command := []string{req.JavaBin, "-cp", strings.Join(classpath, string(os.PathListSeparator)), mainClass}
	for _, arg := range processor.Args {
		resolved, err := resolveToken(arg, placeholders, req.LibrariesDir)
		if err != nil {
			return nil, err
		}
		command = append(command, resolved)
	}
	return command, nil
}

func (i *Installer) extractFile(internal, destDir string) (string, error) {
	name := strings.TrimPrefix(internal, "/")
	reader, err := zip.OpenReader(i.jarPath)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		dest := filepath.Join(destDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", err
		}
		if err := copyZipFile(file, dest); err != nil {
			return "", err
		}
		return dest, nil
	}
	return "", fmt.Errorf("forgeinstaller: %s not found in installer", internal)
}

var placeholderPattern = regexp.MustCompile(`\{[A-Z0-9_]+\}`)

func (i *Installer) extractEmbeddedMaven(librariesDir string) error {
	reader, err := zip.OpenReader(i.jarPath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		relative, ok := strings.CutPrefix(file.Name, "maven/")
		if !ok || file.FileInfo().IsDir() {
			continue
		}
		dest := filepath.Join(librariesDir, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := copyZipFile(file, dest); err != nil {
			return err
		}
	}
	return nil
}

func copyZipFile(file *zip.File, dest string) error {
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

func resolveToken(token string, placeholders map[string]string, librariesDir string) (string, error) {
	if strings.HasPrefix(token, "[") && strings.HasSuffix(token, "]") {
		relative, err := mavenPath(token[1 : len(token)-1])
		if err != nil {
			return "", err
		}
		return filepath.Join(librariesDir, filepath.FromSlash(relative)), nil
	}
	return placeholderPattern.ReplaceAllStringFunc(token, func(match string) string {
		if value, ok := placeholders[match[1:len(match)-1]]; ok {
			return value
		}
		return match
	}), nil
}

func libraryPath(library Library) (string, error) {
	if library.Downloads.Artifact.Path != "" {
		return library.Downloads.Artifact.Path, nil
	}
	return mavenPath(library.Name)
}

func mavenPath(coords string) (string, error) {
	extension := "jar"
	if at := strings.LastIndex(coords, "@"); at >= 0 {
		extension = coords[at+1:]
		coords = coords[:at]
	}
	parts := strings.Split(coords, ":")
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid maven coordinates %q", coords)
	}
	group := strings.ReplaceAll(parts[0], ".", "/")
	artifact, version := parts[1], parts[2]
	file := artifact + "-" + version
	if len(parts) >= 4 {
		file += "-" + parts[3]
	}
	return group + "/" + artifact + "/" + version + "/" + file + "." + extension, nil
}

func processorName(coords string) string {
	parts := strings.Split(coords, ":")
	if len(parts) >= 2 {
		return parts[1]
	}
	return coords
}

func includesClient(sides []string) bool {
	if len(sides) == 0 {
		return true
	}
	for _, side := range sides {
		if side == "client" {
			return true
		}
	}
	return false
}

func jarMainClass(jarPath string) (string, error) {
	reader, err := zip.OpenReader(jarPath)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.Name != "META-INF/MANIFEST.MF" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		scanner := bufio.NewScanner(rc)
		for scanner.Scan() {
			line := scanner.Text()
			if value, ok := strings.CutPrefix(line, "Main-Class:"); ok {
				return strings.TrimSpace(value), nil
			}
		}
	}
	return "", fmt.Errorf("forgeinstaller: no Main-Class in %s", jarPath)
}
