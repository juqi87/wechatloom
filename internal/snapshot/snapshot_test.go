package snapshot_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wechatloom/wechatloom/internal/snapshot"
)

type fakeBrowser struct {
	requests []snapshot.CaptureRequest
}

func (browser *fakeBrowser) Capture(_ context.Context, request snapshot.CaptureRequest) error {
	browser.requests = append(browser.requests, request)
	return os.WriteFile(request.OutputPath, []byte("fake png"), 0o644)
}

func TestCaptureMobileSetUsesTheThreeProductViewports(t *testing.T) {
	t.Parallel()

	buildPath := t.TempDir()
	articlePath := filepath.Join(buildPath, "article.html")
	if err := os.WriteFile(articlePath, []byte("<h1>visual baseline</h1>"), 0o644); err != nil {
		t.Fatalf("write article: %v", err)
	}
	browser := &fakeBrowser{}
	result, err := snapshot.CaptureMobileSet(context.Background(), browser, snapshot.SetRequest{
		ArticleHTMLPath: articlePath,
		OutputDirectory: filepath.Join(buildPath, "snapshots"),
	})
	if err != nil {
		t.Fatalf("CaptureMobileSet() error = %v", err)
	}
	gotWidths := make([]int, 0, len(browser.requests))
	for _, request := range browser.requests {
		gotWidths = append(gotWidths, request.Width)
		if request.Height != 1000 || request.Scale != 2 {
			t.Errorf("capture request = %+v, want height 1000 and scale 2", request)
		}
	}
	if !reflect.DeepEqual(gotWidths, []int{320, 375, 430}) {
		t.Errorf("capture widths = %v, want 320/375/430", gotWidths)
	}
	if len(result.Snapshots) != 3 {
		t.Fatalf("snapshots = %+v, want three", result.Snapshots)
	}
	for _, path := range result.Snapshots {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("stat snapshot %q: %v", path, err)
		}
	}
}
