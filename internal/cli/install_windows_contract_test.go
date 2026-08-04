//go:build windows

package cli_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsInstallerCannotBeRedirectedToAnotherRepository(t *testing.T) {
	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	installerPath, err := filepath.Abs(filepath.Join(repositoryRoot, "scripts", "install.ps1"))
	if err != nil {
		t.Fatalf("resolve installer: %v", err)
	}
	requestLog := filepath.Join(t.TempDir(), "requests.log")
	installDirectory := filepath.Join(t.TempDir(), "bin")
	fixturePath := filepath.Join(t.TempDir(), "wechatloom-fixture.exe")
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
	wrapperPath := filepath.Join(t.TempDir(), "run-installer.ps1")
	wrapper := `
param([string]$Installer, [string]$InstallDir, [string]$RequestLog, [string]$Fixture, [string]$Checksum)
function Invoke-WebRequest {
  param([string]$Uri, [string]$OutFile)
  Add-Content -Path $RequestLog -Value $Uri
  if ($OutFile.EndsWith("SHA256SUMS")) {
    Set-Content -Path $OutFile -Value "$Checksum  wechatloom_1.0.0_windows_amd64.exe"
  } else {
    Copy-Item -Path $Fixture -Destination $OutFile
  }
}
& $Installer -Version "1.0.0" -InstallDir $InstallDir
`
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o600); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-File", wrapperPath, "-Installer", installerPath, "-InstallDir", installDirectory, "-RequestLog", requestLog, "-Fixture", fixturePath, "-Checksum", fixtureSHA256)
	command.Env = append(os.Environ(), "WECHATLOOM_REPOSITORY=attacker/example")
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
	installedPath := filepath.Join(installDirectory, "wechatloom.exe")
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
