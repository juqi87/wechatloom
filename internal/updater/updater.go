package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultManifestURL = "https://github.com/juqi87/wechatloom/releases/latest/download/update-manifest.json"
	maximumManifest    = 1 << 20
	maximumBinary      = 128 << 20
)

type Asset struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	SchemaVersion string  `json:"schema_version"`
	Version       string  `json:"version"`
	PageURL       string  `json:"page_url"`
	Assets        []Asset `json:"assets"`
}

type CheckResult struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	Available      bool   `json:"available"`
	PageURL        string `json:"page_url"`
	AssetURL       string `json:"asset_url"`
	AssetSHA256    string `json:"asset_sha256"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
}

type Client struct {
	httpClient *http.Client
}

type InstallResult struct {
	Version string `json:"version"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
}

func New() *Client {
	return &Client{httpClient: &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if len(via) == 0 || validateReleaseSource(via[0].URL) != nil {
				return errors.New("UPDATE_REDIRECT_UNTRUSTED: redirect chain is not anchored to the WeChatLoom GitHub Release")
			}
			return validateReleaseRedirect(request.URL)
		},
	}}
}

func (client *Client) Check(ctx context.Context, manifestURL, currentVersion string) (CheckResult, error) {
	if strings.TrimSpace(manifestURL) == "" {
		manifestURL = DefaultManifestURL
	}
	parsed, err := url.Parse(manifestURL)
	if err != nil {
		return CheckResult{}, fmt.Errorf("UPDATE_MANIFEST_URL: %w", err)
	}
	if err := validateRemoteURL(parsed); err != nil {
		return CheckResult{}, err
	}
	if err := validateReleaseSource(parsed); err != nil {
		return CheckResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return CheckResult{}, fmt.Errorf("UPDATE_MANIFEST_REQUEST: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return CheckResult{}, errors.New("UPDATE_NETWORK: manifest request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return CheckResult{}, fmt.Errorf("UPDATE_HTTP: unexpected status %d", response.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maximumManifest+1))
	if err != nil {
		return CheckResult{}, fmt.Errorf("UPDATE_MANIFEST_READ: %w", err)
	}
	if len(content) > maximumManifest {
		return CheckResult{}, errors.New("UPDATE_MANIFEST_SIZE: manifest exceeds 1 MiB")
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return CheckResult{}, fmt.Errorf("UPDATE_MANIFEST_INVALID: %w", err)
	}
	if manifest.SchemaVersion != "1" || strings.TrimSpace(manifest.Version) == "" {
		return CheckResult{}, errors.New("UPDATE_MANIFEST_INVALID: schema_version 1 and version are required")
	}
	var selected Asset
	for _, asset := range manifest.Assets {
		if asset.OS == runtime.GOOS && asset.Arch == runtime.GOARCH {
			selected = asset
			break
		}
	}
	if selected.URL == "" {
		return CheckResult{}, fmt.Errorf("UPDATE_ASSET_UNAVAILABLE: no asset for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	assetURL, err := url.Parse(selected.URL)
	if err != nil || validateRemoteURL(assetURL) != nil || validateReleaseSource(assetURL) != nil {
		return CheckResult{}, errors.New("UPDATE_MANIFEST_INVALID: asset URL must be a WeChatLoom GitHub Release")
	}
	checksum, err := hex.DecodeString(selected.SHA256)
	if err != nil || len(checksum) != 32 {
		return CheckResult{}, errors.New("UPDATE_MANIFEST_INVALID: asset sha256 must be 64 hexadecimal characters")
	}
	return CheckResult{
		CurrentVersion: currentVersion, LatestVersion: strings.TrimPrefix(manifest.Version, "v"),
		Available: newer(manifest.Version, currentVersion), PageURL: manifest.PageURL,
		AssetURL: selected.URL, AssetSHA256: strings.ToLower(selected.SHA256), OS: runtime.GOOS, Arch: runtime.GOARCH,
	}, nil
}

func (client *Client) Install(ctx context.Context, checked CheckResult, destination string) (InstallResult, error) {
	if !checked.Available {
		return InstallResult{}, errors.New("UPDATE_NOT_AVAILABLE: manifest does not contain a newer version")
	}
	if strings.TrimSpace(destination) == "" {
		return InstallResult{}, errors.New("UPDATE_DESTINATION_REQUIRED: executable path is required")
	}
	parsed, err := url.Parse(checked.AssetURL)
	if err != nil || validateRemoteURL(parsed) != nil || validateReleaseSource(parsed) != nil {
		return InstallResult{}, errors.New("UPDATE_ASSET_URL_INVALID: asset must be an HTTPS WeChatLoom GitHub Release")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return InstallResult{}, fmt.Errorf("UPDATE_ASSET_REQUEST: %w", err)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return InstallResult{}, errors.New("UPDATE_NETWORK: asset request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return InstallResult{}, fmt.Errorf("UPDATE_HTTP: unexpected asset status %d", response.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maximumBinary+1))
	if err != nil {
		return InstallResult{}, fmt.Errorf("UPDATE_ASSET_READ: %w", err)
	}
	if len(content) > maximumBinary {
		return InstallResult{}, errors.New("UPDATE_ASSET_SIZE: binary exceeds 128 MiB")
	}
	sum := sha256.Sum256(content)
	actual := hex.EncodeToString(sum[:])
	if actual != strings.ToLower(checked.AssetSHA256) {
		return InstallResult{}, errors.New("UPDATE_CHECKSUM_MISMATCH: downloaded binary was not installed")
	}
	absolute, err := filepath.Abs(destination)
	if err != nil {
		return InstallResult{}, fmt.Errorf("resolve update destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("create update directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(absolute), ".wechatloom-update-*")
	if err != nil {
		return InstallResult{}, fmt.Errorf("stage update: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o755); err != nil {
		_ = temporary.Close()
		return InstallResult{}, err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return InstallResult{}, err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return InstallResult{}, err
	}
	if err := temporary.Close(); err != nil {
		return InstallResult{}, err
	}
	backup := absolute + ".previous"
	hadExisting := false
	if _, err := os.Stat(absolute); err == nil {
		hadExisting = true
		_ = os.Remove(backup)
		if err := os.Rename(absolute, backup); err != nil {
			return InstallResult{}, fmt.Errorf("backup current executable: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return InstallResult{}, err
	}
	if err := os.Rename(temporaryPath, absolute); err != nil {
		if hadExisting {
			_ = os.Rename(backup, absolute)
		}
		return InstallResult{}, fmt.Errorf("install update: %w", err)
	}
	removeTemporary = false
	if hadExisting {
		_ = os.Remove(backup)
	}
	return InstallResult{Version: checked.LatestVersion, Path: absolute, SHA256: actual}, nil
}

func validateRemoteURL(value *url.URL) error {
	if value == nil || value.Hostname() == "" {
		return errors.New("UPDATE_URL_INVALID: absolute URL is required")
	}
	if value.Scheme == "https" {
		return nil
	}
	return errors.New("UPDATE_URL_INVALID: HTTPS is required")
}

func validateReleaseSource(value *url.URL) error {
	if value == nil {
		return errors.New("UPDATE_SOURCE_UNTRUSTED: GitHub Releases is the only trusted update source")
	}
	host := strings.ToLower(value.Hostname())
	if host == "github.com" && strings.HasPrefix(filepath.ToSlash(value.Path), "/juqi87/wechatloom/releases/") {
		return nil
	}
	return errors.New("UPDATE_SOURCE_UNTRUSTED: GitHub Releases is the only trusted update source")
}

func validateReleaseRedirect(value *url.URL) error {
	if err := validateRemoteURL(value); err != nil {
		return err
	}
	if validateReleaseSource(value) == nil {
		return nil
	}
	if strings.EqualFold(value.Hostname(), "release-assets.githubusercontent.com") {
		return nil
	}
	return errors.New("UPDATE_REDIRECT_UNTRUSTED: redirect target is not an approved GitHub Release CDN")
}

func newer(candidate, current string) bool {
	left := versionParts(candidate)
	right := versionParts(current)
	for index := 0; index < 3; index++ {
		if left[index] != right[index] {
			return left[index] > right[index]
		}
	}
	return false
}

func versionParts(value string) [3]int {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	value = strings.SplitN(value, "-", 2)[0]
	parts := strings.Split(value, ".")
	var result [3]int
	for index := 0; index < len(parts) && index < 3; index++ {
		result[index], _ = strconv.Atoi(parts[index])
	}
	return result
}
