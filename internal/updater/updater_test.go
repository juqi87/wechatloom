package updater

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestVerifiedGitHubReleaseCanBeCheckedAndAtomicallyInstalled(t *testing.T) {
	binary := []byte("verified-wechatloom-binary")
	sum := sha256.Sum256(binary)
	manifestURL := "https://github.com/wechatloom/wechatloom/releases/download/v0.4.0/update-manifest.json"
	assetURL := "https://github.com/wechatloom/wechatloom/releases/download/v0.4.0/wechatloom"
	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body string
		switch request.URL.String() {
		case manifestURL:
			body = fmt.Sprintf(`{"schema_version":"1","version":"0.4.0","page_url":"https://github.com/wechatloom/wechatloom/releases/tag/v0.4.0","assets":[{"os":%q,"arch":%q,"url":%q,"sha256":%q}]}`, runtime.GOOS, runtime.GOARCH, assetURL, fmt.Sprintf("%x", sum))
		case assetURL:
			body = string(binary)
		default:
			t.Fatalf("unexpected update request: %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}

	checked, err := client.Check(context.Background(), manifestURL, "0.3.0")
	if err != nil {
		t.Fatalf("check update: %v", err)
	}
	if !checked.Available || checked.LatestVersion != "0.4.0" {
		t.Fatalf("checked update = %+v", checked)
	}
	destination := filepath.Join(t.TempDir(), "wechatloom")
	installed, err := client.Install(context.Background(), checked, destination)
	if err != nil {
		t.Fatalf("install update: %v", err)
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("read installed update: %v", err)
	}
	if string(content) != string(binary) || installed.SHA256 != fmt.Sprintf("%x", sum) {
		t.Fatalf("installed update = %+v, content=%q", installed, content)
	}
}

func TestInstallRejectsAConstructedNonReleaseAsset(t *testing.T) {
	client := New()
	_, err := client.Install(context.Background(), CheckResult{
		Available: true, LatestVersion: "0.4.0", AssetURL: "https://example.com/wechatloom",
		AssetSHA256: strings.Repeat("a", 64),
	}, filepath.Join(t.TempDir(), "wechatloom"))
	if err == nil || !strings.Contains(err.Error(), "UPDATE_ASSET_URL_INVALID") {
		t.Fatalf("untrusted install error = %v", err)
	}
}

func TestRedirectsStayAnchoredToTheOfficialReleaseAndApprovedCDN(t *testing.T) {
	client := New()
	root, err := http.NewRequest(http.MethodGet, "https://github.com/wechatloom/wechatloom/releases/latest/download/update-manifest.json", nil)
	if err != nil {
		t.Fatalf("create root request: %v", err)
	}
	approved, err := http.NewRequest(http.MethodGet, "https://release-assets.githubusercontent.com/github-production-release-asset/file", nil)
	if err != nil {
		t.Fatalf("create approved redirect: %v", err)
	}
	if err := client.httpClient.CheckRedirect(approved, []*http.Request{root}); err != nil {
		t.Fatalf("approved GitHub release CDN redirect: %v", err)
	}
	for _, target := range []string{
		"https://example.com/wechatloom",
		"https://github.com/another/repository/releases/download/v1/wechatloom",
	} {
		request, requestErr := http.NewRequest(http.MethodGet, target, nil)
		if requestErr != nil {
			t.Fatalf("create redirect request: %v", requestErr)
		}
		if err := client.httpClient.CheckRedirect(request, []*http.Request{root}); err == nil {
			t.Fatalf("redirect target %q was accepted", target)
		}
	}
}
