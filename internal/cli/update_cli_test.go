package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wechatloom/wechatloom/internal/cli"
)

func TestUpdateRejectsLoopbackManifestSourcesBeforeNetworkAccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := cli.NewRunner(&stdout, &stderr).Run([]string{"update", "check", "--manifest-url", "http://127.0.0.1:1/update-manifest.json", "--json"})
	if exitCode != 1 {
		t.Fatalf("loopback update exit = %d; stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	var response struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if response.Code != "UPDATE_CHECK_FAILED" || !bytes.Contains(stdout.Bytes(), []byte("UPDATE_URL_INVALID")) {
		t.Errorf("loopback update response = %s", stdout.String())
	}
}

func TestUpdateRejectsNonGitHubReleaseSourcesBeforeNetworkAccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := cli.NewRunner(&stdout, &stderr).Run([]string{"update", "check", "--manifest-url", "https://example.com/update-manifest.json", "--json"})
	if exitCode != 1 {
		t.Fatalf("untrusted update exit = %d; stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("UPDATE_SOURCE_UNTRUSTED")) {
		t.Fatalf("untrusted update response = %s", stdout.String())
	}
}

func TestUpdateInstallRequiresConfirmationBeforeAnyNetworkOrFilesystemWrite(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "wechatloom")

	var stdout, stderr bytes.Buffer
	if exitCode := cli.NewRunner(&stdout, &stderr).Run([]string{"update", "install", "--output", destination, "--json"}); exitCode != 2 {
		t.Fatalf("unconfirmed install exit = %d, want 2", exitCode)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("unconfirmed update wrote destination: %v", err)
	}
}
