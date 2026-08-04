package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wechatloom/wechatloom/internal/cli"
)

func TestAccountVerifyUsesNamedUserConfigurationWithoutLeakingSecrets(t *testing.T) {
	const (
		appID     = "wx1234567890abcdef"
		appSecret = "test-app-secret"
		access    = "test-access-token"
	)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/cgi-bin/token" {
			t.Errorf("request path = %q, want /cgi-bin/token", request.URL.Path)
		}
		query := request.URL.Query()
		if query.Get("grant_type") != "client_credential" || query.Get("appid") != appID || query.Get("secret") != appSecret {
			t.Errorf("token query = %v, want configured account credentials", query)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"` + access + `","expires_in":7200}`))
	}))
	defer server.Close()
	t.Setenv("WECHATLOOM_WECHAT_API_BASE_URL", server.URL)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := []byte(`schema_version: "1"
wechat:
  default_account: personal
  accounts:
    personal:
      app_id: wx1234567890abcdef
      app_secret: test-app-secret
`)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	exitCode := cli.NewRunner(&stdout, &stderr).Run([]string{
		"account", "verify", "personal", "--config", configPath, "--json",
	})
	if exitCode != 0 {
		t.Fatalf("account verify exit code = %d; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	var response struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Status  string `json:"status"`
		Data    struct {
			Account        string `json:"account"`
			MaskedAppID    string `json:"masked_app_id"`
			Ready          bool   `json:"ready"`
			TokenExpiresIn int    `json:"token_expires_in"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode account verify response: %v\n%s", err, stdout.String())
	}
	if !response.Success || response.Code != "ACCOUNT_VERIFIED" || response.Status != "ready" {
		t.Errorf("response = %+v, want verified account", response)
	}
	if response.Data.Account != "personal" || response.Data.MaskedAppID != "wx12…cdef" || !response.Data.Ready || response.Data.TokenExpiresIn != 7200 {
		t.Errorf("account readiness = %+v", response.Data)
	}
	combinedOutput := stdout.String() + stderr.String()
	for _, secret := range []string{appSecret, access, appID} {
		if strings.Contains(combinedOutput, secret) {
			t.Errorf("command output leaked sensitive value %q", secret)
		}
	}
}

func TestAccountVerifyNetworkFailureDoesNotLeakCredentials(t *testing.T) {
	const (
		appID     = "wx-network-failure-id"
		appSecret = "network-failure-secret"
	)

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	baseURL := server.URL
	server.Close()
	t.Setenv("WECHATLOOM_WECHAT_API_BASE_URL", baseURL)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := []byte(`schema_version: "1"
wechat:
  default_account: personal
  accounts:
    personal:
      app_id: wx-network-failure-id
      app_secret: network-failure-secret
`)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	exitCode := cli.NewRunner(&stdout, &stderr).Run([]string{
		"account", "verify", "personal", "--config", configPath, "--json",
	})
	if exitCode != 1 {
		t.Fatalf("account verify exit code = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	var response struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode account failure response: %v\n%s", err, stdout.String())
	}
	if response.Success || response.Code != "ACCOUNT_VERIFY_FAILED" {
		t.Errorf("response = %+v, want stable account verification failure", response)
	}
	combinedOutput := stdout.String() + stderr.String()
	for _, secret := range []string{appSecret, appID} {
		if strings.Contains(combinedOutput, secret) {
			t.Errorf("network failure output leaked credential %q: %s", secret, combinedOutput)
		}
	}
}

func TestAccountVerifyClassifiesIPAllowlistErrorsWithoutEchoingRemoteDetails(t *testing.T) {
	const remoteDetail = "invalid ip 203.0.113.7; credential-like-detail"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"errcode":40164,"errmsg":"` + remoteDetail + `"}`))
	}))
	defer server.Close()
	t.Setenv("WECHATLOOM_WECHAT_API_BASE_URL", server.URL)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := []byte(`schema_version: "1"
wechat:
  default_account: personal
  accounts:
    personal:
      app_id: wx-allowlist-test
      app_secret: allowlist-test-secret
`)
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	var stdout, stderr bytes.Buffer
	exitCode := cli.NewRunner(&stdout, &stderr).Run([]string{
		"account", "verify", "--config", configPath, "--json",
	})
	if exitCode != 1 {
		t.Fatalf("account verify exit code = %d, want 1; stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
	}
	combinedOutput := stdout.String() + stderr.String()
	if !strings.Contains(combinedOutput, "WECHAT_IP_NOT_ALLOWED") {
		t.Errorf("account verify output = %s, want stable IP allowlist classification", combinedOutput)
	}
	for _, sensitive := range []string{remoteDetail, "allowlist-test-secret", "wx-allowlist-test"} {
		if strings.Contains(combinedOutput, sensitive) {
			t.Errorf("account verify output leaked %q: %s", sensitive, combinedOutput)
		}
	}
}
