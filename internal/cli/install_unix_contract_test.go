//go:build !windows

package cli_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUnixInstallerCannotBeRedirectedToAnotherRepository(t *testing.T) {
	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	installerPath := filepath.Join(repositoryRoot, "scripts", "install.sh")
	toolDirectory := t.TempDir()
	requestLog := filepath.Join(t.TempDir(), "requests.log")
	installDirectory := filepath.Join(t.TempDir(), "bin")
	fixturePath := filepath.Join(t.TempDir(), "wechatloom-fixture")
	build := exec.Command("go", "build", "-trimpath", "-ldflags=-X github.com/wechatloom/wechatloom/internal/version.Version=1.0.0", "-o", fixturePath, "./cmd/wechatloom")
	build.Dir = repositoryRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build installer fixture: %v\n%s", err, output)
	}
	fixture, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read installer fixture: %v", err)
	}
	digest := sha256.Sum256(fixture)
	fixtureSHA256 := hex.EncodeToString(digest[:])

	fakeCurl := `#!/bin/sh
set -eu
url=""
output=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    http*) url="$1" ;;
    --output) shift; output="$1" ;;
  esac
  shift
done
printf '%s\n' "$url" >> "$WECHATLOOM_TEST_REQUEST_LOG"
case "$output" in
  *SHA256SUMS) printf '%s  wechatloom_1.0.0_%s_%s\n' "$WECHATLOOM_TEST_SHA256" "$WECHATLOOM_TEST_OS" "$WECHATLOOM_TEST_ARCH" > "$output" ;;
  *) cp "$WECHATLOOM_TEST_FIXTURE" "$output" ;;
esac
`
	writeExecutable(t, filepath.Join(toolDirectory, "curl"), fakeCurl)

	osName := "darwin"
	if runtime.GOOS == "linux" {
		osName = "linux"
	}
	arch := "arm64"
	if runtime.GOARCH == "amd64" {
		arch = "amd64"
	}
	command := exec.Command("sh", installerPath)
	command.Env = append(os.Environ(),
		"PATH="+toolDirectory+string(os.PathListSeparator)+os.Getenv("PATH"),
		"WECHATLOOM_VERSION=1.0.0",
		"WECHATLOOM_INSTALL_DIR="+installDirectory,
		"WECHATLOOM_REPOSITORY=attacker/example",
		"WECHATLOOM_TEST_REQUEST_LOG="+requestLog,
		"WECHATLOOM_TEST_OS="+osName,
		"WECHATLOOM_TEST_ARCH="+arch,
		"WECHATLOOM_TEST_FIXTURE="+fixturePath,
		"WECHATLOOM_TEST_SHA256="+fixtureSHA256,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run installer: %v\n%s", err, output)
	}
	requests, err := os.ReadFile(requestLog)
	if err != nil {
		t.Fatalf("read request log: %v", err)
	}
	if strings.Contains(string(requests), "attacker/example") || !strings.Contains(string(requests), "github.com/juqi87/wechatloom/releases/download/v1.0.0") {
		t.Fatalf("installer requests = %q, want only the official repository", requests)
	}
	installedPath := filepath.Join(installDirectory, "wechatloom")
	capabilities, err := exec.Command(installedPath, "capabilities", "--json").Output()
	if err != nil {
		t.Fatalf("run installed binary: %v", err)
	}
	var response struct {
		Data struct {
			Tool struct {
				Version string `json:"version"`
			} `json:"tool"`
		} `json:"data"`
	}
	if err := json.Unmarshal(capabilities, &response); err != nil || response.Data.Tool.Version != "1.0.0" {
		t.Fatalf("installed capabilities = %s, decode error = %v", capabilities, err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}
