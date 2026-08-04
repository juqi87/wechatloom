//go:build windows

package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wechatloom/wechatloom/internal/cli"
)

func TestAccountVerifyAcceptsPrivateConfigOnWindows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"test-token","expires_in":7200}`))
	}))
	defer server.Close()
	t.Setenv("WECHATLOOM_WECHAT_API_BASE_URL", server.URL)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := []byte("schema_version: \"1\"\nwechat:\n  default_account: personal\n  accounts:\n    personal:\n      app_id: wx1234567890abcdef\n      app_secret: test-secret\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	exitCode := cli.NewRunner(&stdout, &stderr).Run([]string{"account", "verify", "--config", configPath, "--json"})
	if exitCode != 0 {
		t.Fatalf("account verify exit = %d; stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
}

func TestAccountVerifyRejectsConfigReadableByEveryoneOnWindows(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := []byte("schema_version: \"1\"\nwechat:\n  default_account: personal\n  accounts:\n    personal:\n      app_id: wx1234567890abcdef\n      app_secret: test-secret\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if output, err := exec.Command("icacls.exe", configPath, "/grant", "*S-1-1-0:R").CombinedOutput(); err != nil {
		t.Fatalf("grant Everyone read permission: %v\n%s", err, output)
	}

	var stdout, stderr bytes.Buffer
	exitCode := cli.NewRunner(&stdout, &stderr).Run([]string{"account", "verify", "--config", configPath, "--json"})
	if exitCode != 1 {
		t.Fatalf("account verify exit = %d, want 1; stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	var response struct {
		Code string `json:"code"`
		Data struct {
			Error string `json:"error"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v\n%s", err, stdout.String())
	}
	if response.Code != "ACCOUNT_VERIFY_FAILED" || !strings.Contains(response.Data.Error, "USER_CONFIG_PERMISSIONS") {
		t.Fatalf("response = %+v, want Windows ACL rejection", response)
	}
}
