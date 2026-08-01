package snapshot_test

import (
	"context"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/wechatloom/wechatloom/internal/builder"
	"github.com/wechatloom/wechatloom/internal/snapshot"
	"github.com/wechatloom/wechatloom/internal/workspace"
)

func TestComponentGalleryVisualBaseline(t *testing.T) {
	if os.Getenv("WECHATLOOM_VISUAL_REGRESSION") != "1" {
		t.Skip("set WECHATLOOM_VISUAL_REGRESSION=1 to run browser pixel comparison")
	}
	projectDir := t.TempDir()
	if _, err := workspace.NewLocal().Init(context.Background(), projectDir); err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	build, err := builder.New().Build(context.Background(), builder.BuildRequest{
		WorkspaceRoot: projectDir,
		SourcePath:    filepath.Join("..", "..", "testdata", "component-gallery.md"),
		Theme:         "tech-cyan",
	})
	if err != nil {
		t.Fatalf("build gallery: %v", err)
	}
	browser, err := snapshot.Discover()
	if err != nil {
		t.Skipf("visual HTML remains testable, but PNG comparison needs a browser: %v", err)
	}
	actualDirectory := filepath.Join(projectDir, "actual")
	result, err := snapshot.CaptureMobileSet(context.Background(), browser, snapshot.SetRequest{
		ArticleHTMLPath: build.ArticleHTMLPath, OutputDirectory: actualDirectory,
	})
	if err != nil {
		t.Fatalf("capture gallery: %v", err)
	}
	for _, actualPath := range result.Snapshots {
		baselinePath := filepath.Join("..", "..", "testdata", "visual", "tech-cyan", filepath.Base(actualPath))
		difference, err := changedPixelRatio(baselinePath, actualPath)
		if err != nil {
			t.Errorf("compare %s: %v", filepath.Base(actualPath), err)
			continue
		}
		if difference > 0.005 {
			t.Errorf("%s changed pixel ratio = %.3f%%, want at most 0.5%%; actual: %s", filepath.Base(actualPath), difference*100, actualPath)
		}
	}
}

func changedPixelRatio(leftPath, rightPath string) (float64, error) {
	left, err := decodeImage(leftPath)
	if err != nil {
		return 0, err
	}
	right, err := decodeImage(rightPath)
	if err != nil {
		return 0, err
	}
	if left.Bounds() != right.Bounds() {
		return 1, fmt.Errorf("dimensions differ: %v != %v", left.Bounds(), right.Bounds())
	}
	changed, total := 0, 0
	for y := left.Bounds().Min.Y; y < left.Bounds().Max.Y; y++ {
		for x := left.Bounds().Min.X; x < left.Bounds().Max.X; x++ {
			leftColor := color.NRGBAModel.Convert(left.At(x, y)).(color.NRGBA)
			rightColor := color.NRGBAModel.Convert(right.At(x, y)).(color.NRGBA)
			delta := channelDelta(leftColor.R, rightColor.R) + channelDelta(leftColor.G, rightColor.G) + channelDelta(leftColor.B, rightColor.B) + channelDelta(leftColor.A, rightColor.A)
			if delta > 24 {
				changed++
			}
			total++
		}
	}
	return float64(changed) / float64(total), nil
}

func decodeImage(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoded, _, err := image.Decode(file)
	return decoded, err
}

func channelDelta(left, right uint8) int {
	if left > right {
		return int(left - right)
	}
	return int(right - left)
}
