package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"google.golang.org/protobuf/proto"
)

type indexedManifest struct {
	path   string
	active bool
}

func (c *Catalog) index() (string, []indexedManifest, error) {
	listed, err := os.ReadDir(c.dir)
	if err != nil {
		return "", nil, err
	}
	managed := map[string]bool{}
	var entries []indexedManifest
	var stamp strings.Builder
	for _, entry := range listed {
		if !entry.IsDir() {
			continue
		}
		build := entry.Name()
		pointer, err := ReadRelease(c.dir, build)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		managed[build] = true
		for i, id := range append([]string{pointer.Current}, pointer.Retained...) {
			dir := filepath.Join(releaseRoot(c.dir, build), "releases", id)
			if err := VerifyRelease(dir); err != nil {
				return "", nil, err
			}
			files, err := os.ReadDir(dir)
			if err != nil {
				return "", nil, err
			}
			count := 0
			for _, file := range files {
				if !strings.HasSuffix(file.Name(), manifestExt) {
					continue
				}
				path := filepath.Join(dir, file.Name())
				data, err := os.ReadFile(path)
				if err != nil {
					return "", nil, err
				}
				var m corev1.Manifest
				if err := proto.Unmarshal(data, &m); err != nil {
					return "", nil, err
				}
				if m.Modpack != build || file.Name() != ManifestName(build, m.Platform) {
					return "", nil, fmt.Errorf("повреждена публикация %s", build)
				}
				sig, err := os.ReadFile(SignaturePath(path))
				if err != nil {
					return "", nil, err
				}
				if len(sig) == 0 {
					return "", nil, fmt.Errorf("пуста подпись %s", path)
				}
				entries = append(entries, indexedManifest{path: path, active: i == 0})
				count++
			}
			if count == 0 {
				return "", nil, fmt.Errorf("пуста публикация %s", build)
			}
			fmt.Fprintf(&stamp, "%s/%s:%t\n", build, id, i == 0)
		}
	}
	for _, entry := range listed {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), manifestExt) {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), manifestExt)
		skip := false
		for build := range managed {
			if name == build || strings.HasPrefix(name, build+".") {
				data, err := os.ReadFile(filepath.Join(c.dir, entry.Name()))
				var m corev1.Manifest
				if err == nil && proto.Unmarshal(data, &m) == nil && m.Modpack == build {
					skip = true
				}
			}
		}
		if skip {
			continue
		}
		path := filepath.Join(c.dir, entry.Name())
		entries = append(entries, indexedManifest{path: path, active: true})
		for _, file := range []string{path, SignaturePath(path)} {
			info, err := os.Stat(file)
			if err != nil {
				fmt.Fprintf(&stamp, "%s:%v\n", file, err)
				continue
			}
			fmt.Fprintf(&stamp, "%s:%d:%d\n", file, info.ModTime().UnixNano(), info.Size())
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	return stamp.String(), entries, nil
}
