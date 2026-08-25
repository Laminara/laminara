package selfupdate_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/selfupdate"
)

func fakeBinary(reportedVersion string) []byte {
	return []byte("#!/bin/sh\necho " + reportedVersion + "\n")
}

func serve(t *testing.T, tag string, binary []byte, checksum string, withAsset bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	if checksum == "" {
		sum := sha256.Sum256(binary)
		checksum = hex.EncodeToString(sum[:])
	}

	mux.HandleFunc("/binary", func(w http.ResponseWriter, _ *http.Request) { w.Write(binary) })
	mux.HandleFunc("/checksums", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", checksum, selfupdate.AssetName())
	})
	mux.HandleFunc("/repos/owner/repo/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		assets := fmt.Sprintf(`{"name":%q,"browser_download_url":%q}`, "checksums.txt", server.URL+"/checksums")
		if withAsset {
			assets = fmt.Sprintf(`{"name":%q,"browser_download_url":%q},`, selfupdate.AssetName(), server.URL+"/binary") + assets
		}
		fmt.Fprintf(w, `{"tag_name":%q,"body":"свежая версия","assets":[%s]}`, tag, assets)
	})
	return server
}

func checker(server *httptest.Server) *selfupdate.Checker {
	return &selfupdate.Checker{Repo: "owner/repo", API: server.URL}
}

func TestLatestReadsTheRelease(t *testing.T) {
	server := serve(t, "v1.4.0", fakeBinary("1.4.0"), "", true)

	release, err := checker(server).Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "1.4.0" || release.Tag != "v1.4.0" {
		t.Fatalf("release = %s (%s), want 1.4.0 (v1.4.0)", release.Version, release.Tag)
	}
	if !release.IsNewerThan("1.3.9") || release.IsNewerThan("1.4.0") {
		t.Fatal("IsNewerThan compares the release against the running version incorrectly")
	}
}

func TestLatestFailsWhenThisSystemIsMissing(t *testing.T) {
	server := serve(t, "v1.4.0", fakeBinary("1.4.0"), "", false)

	_, err := checker(server).Latest(context.Background())
	if err == nil {
		t.Fatal("a release without a file for this system must be refused")
	}
	if !strings.Contains(err.Error(), selfupdate.AssetName()) {
		t.Fatalf("error must name the missing file, got %v", err)
	}
}

func TestDownloadRefusesAWrongChecksum(t *testing.T) {
	server := serve(t, "v1.4.0", fakeBinary("1.4.0"), strings.Repeat("00", 32), true)
	client := checker(server)

	release, err := client.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := client.Download(context.Background(), release, dir); err == nil {
		t.Fatal("a file whose checksum does not match must be refused")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("the refused download must not be left on disk")
	}
}

func TestDownloadRefusesAReleaseCarryingTheWrongVersion(t *testing.T) {
	server := serve(t, "v1.4.0", fakeBinary("1.3.0"), "", true)
	client := checker(server)

	release, err := client.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Download(context.Background(), release, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "1.3.0") {
		t.Fatalf("a binary that reports another version must be refused, got %v", err)
	}
}

func TestDownloadKeepsAGoodRelease(t *testing.T) {
	server := serve(t, "v1.4.0", fakeBinary("1.4.0"), "", true)
	client := checker(server)

	release, err := client.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	staged, err := client.Download(context.Background(), release, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(staged)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("mode = %v, want an executable file", info.Mode().Perm())
	}
}

func TestInstallSwapsAndKeepsThePreviousBinary(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "laminara-server")
	staged := filepath.Join(dir, "staged")

	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := selfupdate.Install(staged, target); err != nil {
		t.Fatal(err)
	}

	current, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != "new" {
		t.Fatalf("binary = %q, want the downloaded one", current)
	}
	previous, err := os.ReadFile(target + ".old")
	if err != nil {
		t.Fatal("the previous binary must stay next to the new one for a manual rollback")
	}
	if string(previous) != "old" {
		t.Fatalf("previous = %q, want the replaced binary", previous)
	}
}
