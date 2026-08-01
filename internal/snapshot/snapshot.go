package snapshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Browser interface {
	Capture(context.Context, CaptureRequest) error
}

type CaptureRequest struct {
	HTMLPath   string `json:"html_path"`
	OutputPath string `json:"output_path"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Scale      int    `json:"scale"`
}

type SetRequest struct {
	ArticleHTMLPath string `json:"article_html_path"`
	OutputDirectory string `json:"output_directory"`
}

type SetResult struct {
	Snapshots []string `json:"snapshots"`
}

func CaptureMobileSet(ctx context.Context, browser Browser, request SetRequest) (SetResult, error) {
	if err := ctx.Err(); err != nil {
		return SetResult{}, err
	}
	articlePath, err := filepath.Abs(request.ArticleHTMLPath)
	if err != nil {
		return SetResult{}, fmt.Errorf("resolve article HTML: %w", err)
	}
	if info, err := os.Stat(articlePath); err != nil || !info.Mode().IsRegular() {
		return SetResult{}, fmt.Errorf("article HTML is required: %s", articlePath)
	}
	outputDirectory, err := filepath.Abs(request.OutputDirectory)
	if err != nil {
		return SetResult{}, fmt.Errorf("resolve snapshot directory: %w", err)
	}
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		return SetResult{}, fmt.Errorf("create snapshot directory: %w", err)
	}

	result := SetResult{Snapshots: make([]string, 0, 3)}
	for _, width := range []int{320, 375, 430} {
		outputPath := filepath.Join(outputDirectory, "mobile-"+strconv.Itoa(width)+".png")
		if err := browser.Capture(ctx, CaptureRequest{
			HTMLPath: articlePath, OutputPath: outputPath, Width: width, Height: 1000, Scale: 2,
		}); err != nil {
			return SetResult{}, fmt.Errorf("capture %dpx snapshot: %w", width, err)
		}
		result.Snapshots = append(result.Snapshots, outputPath)
	}
	return result, nil
}

type LocalBrowser struct {
	Executable string
}

func Discover() (LocalBrowser, error) {
	if configured := strings.TrimSpace(os.Getenv("WECHATLOOM_BROWSER")); configured != "" {
		if info, err := os.Stat(configured); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return LocalBrowser{Executable: configured}, nil
		}
		return LocalBrowser{}, fmt.Errorf("WECHATLOOM_BROWSER does not point to an executable file: %s", configured)
	}
	candidates := []string{}
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	case "windows":
		for _, root := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")} {
			if root != "" {
				candidates = append(candidates,
					filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"),
					filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"),
				)
			}
		}
	default:
		for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "microsoft-edge"} {
			if path, err := exec.LookPath(name); err == nil {
				candidates = append(candidates, path)
			}
		}
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return LocalBrowser{Executable: candidate}, nil
		}
	}
	return LocalBrowser{}, errors.New("Chrome, Chromium, or Edge was not found; HTML preview is still available")
}

func (browser LocalBrowser) Capture(ctx context.Context, request CaptureRequest) error {
	if strings.TrimSpace(browser.Executable) == "" {
		return errors.New("browser executable is required")
	}
	if request.Width <= 0 || request.Height <= 0 || request.Scale <= 0 {
		return errors.New("positive width, height, and scale are required")
	}
	htmlPath, err := filepath.Abs(request.HTMLPath)
	if err != nil {
		return err
	}
	outputPath, err := filepath.Abs(request.OutputPath)
	if err != nil {
		return err
	}
	if strings.ToLower(filepath.Ext(outputPath)) != ".png" {
		return errors.New("snapshot output must use the .png extension")
	}
	articleHTML, err := os.ReadFile(htmlPath)
	if err != nil {
		return fmt.Errorf("read snapshot HTML: %w", err)
	}
	wrapper, err := os.CreateTemp(filepath.Dir(htmlPath), ".wechatloom-snapshot-wrapper-*.html")
	if err != nil {
		return err
	}
	wrapperPath := wrapper.Name()
	defer os.Remove(wrapperPath)
	wrapped := fmt.Sprintf("<!doctype html><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><style>*{box-sizing:border-box}html,body{margin:0;max-width:%dpx;overflow-x:hidden;width:%dpx}body{background:#fff;padding:16px}</style>%s", request.Width, request.Width, articleHTML)
	if _, err := wrapper.WriteString(wrapped); err != nil {
		_ = wrapper.Close()
		return err
	}
	if err := wrapper.Close(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(outputPath), ".wechatloom-snapshot-*.png")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Remove(temporaryPath); err != nil {
		return err
	}
	defer os.Remove(temporaryPath)
	profilePath, err := os.MkdirTemp("", "wechatloom-chrome-profile-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(profilePath)
	fileURL := (&url.URL{Scheme: "file", Path: wrapperPath}).String()
	command := exec.CommandContext(ctx, browser.Executable,
		"--headless",
		"--disable-background-networking",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-extensions",
		"--disable-gpu",
		"--hide-scrollbars",
		"--no-first-run",
		"--no-pings",
		"--timeout=5000",
		"--user-data-dir="+profilePath,
		"--force-device-scale-factor="+strconv.Itoa(request.Scale),
		fmt.Sprintf("--window-size=%d,%d", request.Width, request.Height),
		"--screenshot="+temporaryPath,
		fileURL,
	)
	var diagnostics bytes.Buffer
	command.Stdout = &diagnostics
	command.Stderr = &diagnostics
	if err := command.Start(); err != nil {
		return fmt.Errorf("start browser screenshot: %w", err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(20 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case err := <-waited:
			if validPNG(temporaryPath) {
				return commitPNG(temporaryPath, outputPath)
			}
			return fmt.Errorf("browser screenshot failed: %v: %s", err, strings.TrimSpace(diagnostics.String()))
		case <-ticker.C:
			if validPNG(temporaryPath) {
				_ = command.Process.Kill()
				<-waited
				return commitPNG(temporaryPath, outputPath)
			}
		case <-ctx.Done():
			_ = command.Process.Kill()
			<-waited
			return ctx.Err()
		case <-timeout.C:
			_ = command.Process.Kill()
			<-waited
			return fmt.Errorf("browser did not create snapshot within 20 seconds: %s", strings.TrimSpace(diagnostics.String()))
		}
	}
}

func commitPNG(temporaryPath, outputPath string) error {
	if runtime.GOOS == "windows" {
		_ = os.Remove(outputPath)
	}
	if err := os.Rename(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("commit snapshot: %w", err)
	}
	return nil
}

func validPNG(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() == 0 {
		return false
	}
	_, err = png.DecodeConfig(file)
	return err == nil
}
