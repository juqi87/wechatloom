package preview_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wechatloom/wechatloom/internal/preview"
)

func TestServerExposesOnlyTheCompletedPreviewOverLoopback(t *testing.T) {
	t.Parallel()

	buildPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(buildPath, "preview.html"), []byte("<h1>local preview</h1>"), 0o644); err != nil {
		t.Fatalf("write preview: %v", err)
	}
	if err := os.WriteFile(filepath.Join(buildPath, "manifest.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(buildPath, "assets"), 0o755); err != nil {
		t.Fatalf("create assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(buildPath, "assets", "image.png"), []byte("png bytes"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	session, err := preview.Start(context.Background(), preview.StartRequest{BuildPath: buildPath})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = session.Close(context.Background()) })
	if !strings.HasPrefix(session.URL(), "http://127.0.0.1:") {
		t.Errorf("URL = %q, want loopback only", session.URL())
	}

	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Get(session.URL())
	if err != nil {
		t.Fatalf("GET preview: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatalf("read preview response: %v", err)
	}
	if response.StatusCode != http.StatusOK || string(body) != "<h1>local preview</h1>" {
		t.Errorf("GET response = %d %q", response.StatusCode, body)
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", response.Header.Get("Cache-Control"))
	}
	asset, err := client.Get(session.URL() + "assets/image.png")
	if err != nil {
		t.Fatalf("GET asset: %v", err)
	}
	assetBody, _ := io.ReadAll(asset.Body)
	_ = asset.Body.Close()
	if asset.StatusCode != http.StatusOK || string(assetBody) != "png bytes" {
		t.Errorf("asset response = %d %q", asset.StatusCode, assetBody)
	}

	post, err := http.Post(session.URL(), "text/plain", strings.NewReader("mutation"))
	if err != nil {
		t.Fatalf("POST preview: %v", err)
	}
	_ = post.Body.Close()
	if post.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", post.StatusCode)
	}
	unknown, err := client.Get(session.URL() + "manifest.json")
	if err != nil {
		t.Fatalf("GET unknown path: %v", err)
	}
	_ = unknown.Body.Close()
	if unknown.StatusCode != http.StatusNotFound {
		t.Errorf("unknown path status = %d, want 404", unknown.StatusCode)
	}
}
