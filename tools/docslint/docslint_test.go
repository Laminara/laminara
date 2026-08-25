package docslint_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const siteURL = "https://docs.laminara.dev"

var linkPattern = regexp.MustCompile(`\]\(([^)\s]+)`)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func pages(t *testing.T, root string) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	docs := filepath.Join(root, "docs")
	err := filepath.WalkDir(docs, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".mdx" {
			return err
		}
		slug, relErr := filepath.Rel(docs, path)
		if relErr != nil {
			return relErr
		}
		found[strings.TrimSuffix(filepath.ToSlash(slug), ".mdx")] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("no pages found under docs/")
	}
	return found
}

func markdownFiles(t *testing.T, root string) []string {
	t.Helper()
	files := []string{filepath.Join(root, "README.md")}
	for slug := range pages(t, root) {
		files = append(files, filepath.Join(root, "docs", slug+".mdx"))
	}
	return files
}

func TestEveryPageIsInTheSidebar(t *testing.T) {
	root := repoRoot(t)
	config, err := os.ReadFile(filepath.Join(root, "site", "astro.config.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	sidebar := string(config)

	for slug := range pages(t, root) {
		if !strings.Contains(sidebar, `"`+slug+`"`) {
			t.Errorf("docs/%s.mdx is not in the sidebar of site/astro.config.mjs, so the page ships unreachable", slug)
		}
	}
}

func TestLinksResolve(t *testing.T) {
	root := repoRoot(t)
	known := pages(t, root)

	for _, file := range markdownFiles(t, root) {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		name, err := filepath.Rel(root, file)
		if err != nil {
			t.Fatal(err)
		}

		for _, match := range linkPattern.FindAllStringSubmatch(string(body), -1) {
			target := match[1]
			switch {
			case strings.HasPrefix(target, siteURL), strings.HasPrefix(target, "/"):
				slug := strings.Trim(strings.TrimPrefix(target, siteURL), "/")
				slug = strings.Trim(strings.SplitN(slug, "#", 2)[0], "/")
				if slug == "" {
					slug = "index"
				}
				if known[slug] {
					continue
				}
				if _, err := os.Stat(filepath.Join(root, "docs", filepath.FromSlash(slug))); err == nil {
					continue
				}
				t.Errorf("%s links to %s, but neither docs/%s.mdx nor docs/%s exists", name, target, slug, slug)
			case strings.HasPrefix(target, "http://"), strings.HasPrefix(target, "https://"), strings.HasPrefix(target, "#"), strings.HasPrefix(target, "mailto:"):
			case strings.HasSuffix(target, ".mdx"):
				t.Errorf("%s links to the file %s; documentation links must point at %s", name, target, siteURL)
			default:
				path := filepath.Join(filepath.Dir(file), filepath.FromSlash(strings.SplitN(target, "#", 2)[0]))
				if _, err := os.Stat(path); err != nil {
					t.Errorf("%s links to %s, which does not exist", name, target)
				}
			}
		}
	}
}
