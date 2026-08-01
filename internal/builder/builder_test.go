package builder_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wechatloom/wechatloom/internal/builder"
	"github.com/wechatloom/wechatloom/internal/catalog"
	"github.com/wechatloom/wechatloom/internal/workspace"
)

func TestInspectReturnsPublishingMetadataWithoutChangingTheSource(t *testing.T) {
	t.Parallel()

	sourcePath := filepath.Join("..", "..", "testdata", "article.md")

	inspection, err := builder.New().Inspect(context.Background(), builder.InspectRequest{
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}

	if inspection.Title != "用 WeChatLoom 构建第一篇文章" {
		t.Errorf("Title = %q, want frontmatter title", inspection.Title)
	}
	if inspection.Author != "WeChatLoom" {
		t.Errorf("Author = %q, want %q", inspection.Author, "WeChatLoom")
	}
	if inspection.SourceHash != "2ccd6edda3d986cd9d9604e61a5c544ff1d2489efe4f522e7c029e6e62bdd0a7" {
		t.Errorf("SourceHash = %q, want stable fixture hash", inspection.SourceHash)
	}
	if inspection.CalloutCount != 1 {
		t.Errorf("CalloutCount = %d, want 1", inspection.CalloutCount)
	}
	if len(inspection.Errors) != 0 {
		t.Errorf("Errors = %v, want none", inspection.Errors)
	}
}

func TestBuildProducesGoldenWeChatHTMLAndPreservesTheSource(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	if _, err := workspace.NewLocal().Init(context.Background(), projectDir); err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}

	sourcePath := filepath.Join("..", "..", "testdata", "article.md")
	sourceBefore, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source before build: %v", err)
	}

	result, err := builder.New().Build(context.Background(), builder.BuildRequest{
		WorkspaceRoot: projectDir,
		SourcePath:    sourcePath,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	gotHTML, err := os.ReadFile(result.ArticleHTMLPath)
	if err != nil {
		t.Fatalf("read article HTML: %v", err)
	}
	wantHTML, err := os.ReadFile(filepath.Join("..", "..", "testdata", "article.golden.html"))
	if err != nil {
		t.Fatalf("read golden HTML: %v", err)
	}
	if !bytes.Equal(gotHTML, wantHTML) {
		t.Errorf("article HTML differs from golden output\n--- got ---\n%s\n--- want ---\n%s", gotHTML, wantHTML)
	}

	sourceAfter, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source after build: %v", err)
	}
	if !bytes.Equal(sourceAfter, sourceBefore) {
		t.Error("Build() changed the source Markdown")
	}
}

func TestBuildIsReproducibleAndManifestRecordsItsInputs(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	if _, err := workspace.NewLocal().Init(context.Background(), projectDir); err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	request := builder.BuildRequest{
		WorkspaceRoot: projectDir,
		SourcePath:    filepath.Join("..", "..", "testdata", "article.md"),
	}

	first, err := builder.New().Build(context.Background(), request)
	if err != nil {
		t.Fatalf("first Build() error = %v", err)
	}
	second, err := builder.New().Build(context.Background(), request)
	if err != nil {
		t.Fatalf("second Build() error = %v", err)
	}
	if first.ContentHash != second.ContentHash {
		t.Errorf("content hashes differ: %q != %q", first.ContentHash, second.ContentHash)
	}
	firstHTML, err := os.ReadFile(first.ArticleHTMLPath)
	if err != nil {
		t.Fatalf("read first HTML: %v", err)
	}
	secondHTML, err := os.ReadFile(second.ArticleHTMLPath)
	if err != nil {
		t.Fatalf("read second HTML: %v", err)
	}
	if !bytes.Equal(firstHTML, secondHTML) {
		t.Error("the same locked input produced different HTML")
	}

	manifestBytes, err := os.ReadFile(second.ManifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest struct {
		SchemaVersion   string `json:"schema_version"`
		ProtocolVersion string `json:"protocol_version"`
		Tool            struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"tool"`
		Source struct {
			SHA256 string `json:"sha256"`
		} `json:"source"`
		DerivedMarkdownSHA256 string `json:"derived_markdown_sha256"`
		Theme                 struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"theme"`
		Components []struct {
			Name          string `json:"name"`
			SchemaVersion string `json:"schema_version"`
		} `json:"components"`
		Artifacts []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("decode manifest: %v\n%s", err, manifestBytes)
	}

	if manifest.SchemaVersion != "1" || manifest.ProtocolVersion != "1.0" {
		t.Errorf(
			"manifest versions = schema %q, protocol %q; want 1 and 1.0",
			manifest.SchemaVersion,
			manifest.ProtocolVersion,
		)
	}
	if manifest.Tool.Name != "wechatloom" || manifest.Tool.Version == "" {
		t.Errorf("manifest tool = %+v, want named version", manifest.Tool)
	}
	if manifest.Source.SHA256 != second.SourceHash {
		t.Errorf("manifest source hash = %q, want %q", manifest.Source.SHA256, second.SourceHash)
	}
	if manifest.DerivedMarkdownSHA256 != second.SourceHash {
		t.Errorf(
			"derived Markdown hash = %q, want copied source hash %q",
			manifest.DerivedMarkdownSHA256,
			second.SourceHash,
		)
	}
	if manifest.Theme.Name != "minimal" || manifest.Theme.Version == "" {
		t.Errorf("manifest theme = %+v, want versioned minimal theme", manifest.Theme)
	}
	if len(manifest.Components) != 1 ||
		manifest.Components[0].Name != "callout" ||
		manifest.Components[0].SchemaVersion != "1" {
		t.Errorf("manifest components = %+v, want versioned callout", manifest.Components)
	}

	articleHash := ""
	for _, artifact := range manifest.Artifacts {
		if artifact.Path == "article.html" {
			articleHash = artifact.SHA256
		}
	}
	if articleHash != second.ContentHash {
		t.Errorf("article artifact hash = %q, want %q", articleHash, second.ContentHash)
	}
}

func TestInspectReportsInvalidComponentAtItsSourceLine(t *testing.T) {
	t.Parallel()

	sourcePath := filepath.Join(t.TempDir(), "invalid-callout.md")
	source := []byte(`# Invalid component

:::wx-callout
tone: info
content: 缺少必填标题。
:::
`)
	if err := os.WriteFile(sourcePath, source, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	inspection, err := builder.New().Inspect(context.Background(), builder.InspectRequest{
		SourcePath: sourcePath,
	})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if len(inspection.Errors) != 1 {
		t.Fatalf("Errors = %+v, want one component diagnostic", inspection.Errors)
	}
	if inspection.Errors[0].Code != "COMPONENT_SCHEMA_INVALID" {
		t.Errorf(
			"diagnostic code = %q, want %q",
			inspection.Errors[0].Code,
			"COMPONENT_SCHEMA_INVALID",
		)
	}
	if inspection.Errors[0].Line != 3 {
		t.Errorf("diagnostic line = %d, want 3", inspection.Errors[0].Line)
	}
}

func TestInspectAcceptsTheCompleteComponentGallery(t *testing.T) {
	t.Parallel()

	inspection, err := builder.New().Inspect(context.Background(), builder.InspectRequest{
		SourcePath: filepath.Join("..", "..", "testdata", "component-gallery.md"),
	})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if len(inspection.Errors) != 0 {
		t.Fatalf("inspection errors = %+v, want none", inspection.Errors)
	}
	if inspection.ComponentCount != 24 {
		t.Errorf("ComponentCount = %d, want 24", inspection.ComponentCount)
	}
}

func TestBuildRendersAllComponentsWithTheSelectedTheme(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	if _, err := workspace.NewLocal().Init(context.Background(), projectDir); err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	result, err := builder.New().Build(context.Background(), builder.BuildRequest{
		WorkspaceRoot: projectDir,
		SourcePath:    filepath.Join("..", "..", "testdata", "component-gallery.md"),
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	htmlBytes, err := os.ReadFile(result.ArticleHTMLPath)
	if err != nil {
		t.Fatalf("read article HTML: %v", err)
	}
	articleHTML := string(htmlBytes)
	if !strings.Contains(articleHTML, `data-wechatloom-theme="tech-cyan"`) ||
		!strings.Contains(articleHTML, "background:#F5FCFD") ||
		!strings.Contains(articleHTML, "#0E7490") {
		t.Errorf("article does not use the selected tech-cyan tokens:\n%s", articleHTML)
	}
	if strings.Contains(articleHTML, `font-family:-apple-system,BlinkMacSystemFont,"Segoe UI"`) {
		t.Error("article contains an unescaped quote inside a style attribute")
	}
	if !strings.Contains(articleHTML, "overflow-wrap:anywhere") {
		t.Error("article headings do not protect narrow mobile viewports from overflow")
	}
	componentNames := []string{
		"hero", "lead", "toc", "audience", "section", "divider", "steps", "timeline",
		"checklist", "callout", "quote", "metrics", "compare", "case", "pros-cons",
		"image-text", "gallery", "code-card", "data-card", "summary", "takeaways",
		"author", "cta", "subscribe",
	}
	for _, name := range componentNames {
		marker := `data-wx-component="` + name + `"`
		if count := strings.Count(articleHTML, marker); count != 1 {
			t.Errorf("%s count = %d, want 1", marker, count)
		}
	}
	if strings.Contains(articleHTML, ":::wx-") {
		t.Error("article contains unrendered component directives")
	}
	if strings.Contains(articleHTML, `src="./images/`) {
		t.Errorf("article contains unresolved local component images:\n%s", articleHTML)
	}
	localAssets, err := filepath.Glob(filepath.Join(result.BuildPath, "assets", "image-*.png"))
	if err != nil {
		t.Fatalf("glob local image assets: %v", err)
	}
	if len(localAssets) != 4 {
		t.Errorf("resolved local images = %v, want four content-addressed assets", localAssets)
	}

	manifestBytes, err := os.ReadFile(result.ManifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest struct {
		Theme struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Tokens  struct {
				Colors struct {
					Accent string `json:"accent"`
				} `json:"colors"`
			} `json:"tokens"`
		} `json:"theme"`
		Components []struct {
			Name string `json:"name"`
		} `json:"components"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Theme.Name != "tech-cyan" || manifest.Theme.Version != "0.2.0" {
		t.Errorf("manifest theme = %+v, want tech-cyan 0.2.0", manifest.Theme)
	}
	if manifest.Theme.Tokens.Colors.Accent != "#0E7490" {
		t.Errorf("manifest theme tokens = %+v, want resolved design variables", manifest.Theme.Tokens)
	}
	if len(manifest.Components) != 24 {
		t.Errorf("manifest component definitions = %d, want 24", len(manifest.Components))
	}
}

func TestBuildReadsThemeFromConfigAndAllowsAnExplicitOverride(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	if _, err := workspace.NewLocal().Init(context.Background(), projectDir); err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	configPath := filepath.Join(projectDir, ".wechatloom", "project.yaml")
	config := []byte(`schema_version: "1"
project:
  name: theme-priority
build:
  theme: business-teal
  output_dir: .wechatloom/builds
`)
	if err := os.WriteFile(configPath, config, 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}
	sourcePath := filepath.Join(projectDir, "article.md")
	if err := os.WriteFile(sourcePath, []byte("# Configured theme\n\nBody.\n"), 0o644); err != nil {
		t.Fatalf("write article: %v", err)
	}

	configured, err := builder.New().Build(context.Background(), builder.BuildRequest{
		WorkspaceRoot: projectDir,
		SourcePath:    sourcePath,
	})
	if err != nil {
		t.Fatalf("Build(configured) error = %v", err)
	}
	configuredHTML, err := os.ReadFile(configured.ArticleHTMLPath)
	if err != nil {
		t.Fatalf("read configured HTML: %v", err)
	}
	if !bytes.Contains(configuredHTML, []byte(`data-wechatloom-theme="business-teal"`)) {
		t.Errorf("configured build did not use business-teal:\n%s", configuredHTML)
	}

	overridden, err := builder.New().Build(context.Background(), builder.BuildRequest{
		WorkspaceRoot: projectDir,
		SourcePath:    sourcePath,
		Theme:         "culture-jade",
	})
	if err != nil {
		t.Fatalf("Build(overridden) error = %v", err)
	}
	overriddenHTML, err := os.ReadFile(overridden.ArticleHTMLPath)
	if err != nil {
		t.Fatalf("read overridden HTML: %v", err)
	}
	if !bytes.Contains(overriddenHTML, []byte(`data-wechatloom-theme="culture-jade"`)) {
		t.Errorf("explicit build did not override config:\n%s", overriddenHTML)
	}
}

func TestPreviewProvidesOfflineThemeAndMobileViewportControls(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	if _, err := workspace.NewLocal().Init(context.Background(), projectDir); err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	result, err := builder.New().Build(context.Background(), builder.BuildRequest{
		WorkspaceRoot: projectDir,
		SourcePath:    filepath.Join("..", "..", "testdata", "component-gallery.md"),
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	previewBytes, err := os.ReadFile(result.PreviewHTMLPath)
	if err != nil {
		t.Fatalf("read preview HTML: %v", err)
	}
	preview := string(previewBytes)
	for _, marker := range []string{
		`data-preview-control="theme"`,
		`data-preview-width="320"`,
		`data-preview-width="375"`,
		`data-preview-width="430"`,
		`data-preview-frame`,
		"最终视觉以微信公众号草稿箱渲染为准",
	} {
		if !strings.Contains(preview, marker) {
			t.Errorf("preview is missing %q", marker)
		}
	}
	if count := strings.Count(preview, `<option value=`); count != 24 {
		t.Errorf("theme option count = %d, want 24", count)
	}
	for _, remote := range []string{`<script src=`, `<link rel="stylesheet"`, `src="http://`, `src="https://`} {
		if strings.Contains(preview, remote) {
			t.Errorf("preview unexpectedly depends on remote content %q", remote)
		}
	}
}

func TestBuildRendersFormulaAndMermaidToCachedHighResolutionPNG(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	if _, err := workspace.NewLocal().Init(context.Background(), projectDir); err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	result, err := builder.New().Build(context.Background(), builder.BuildRequest{
		WorkspaceRoot: projectDir,
		SourcePath:    filepath.Join("..", "..", "testdata", "rich-media.md"),
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	articleBytes, err := os.ReadFile(result.ArticleHTMLPath)
	if err != nil {
		t.Fatalf("read article HTML: %v", err)
	}
	articleHTML := string(articleBytes)
	for _, marker := range []string{"assets/formula-", "assets/mermaid-", `alt="公式：E = mc^2"`, `alt="流程图：Markdown → Build → Preview"`} {
		if !strings.Contains(articleHTML, marker) {
			t.Errorf("article is missing %q:\n%s", marker, articleHTML)
		}
	}
	assets, err := filepath.Glob(filepath.Join(result.BuildPath, "assets", "*.png"))
	if err != nil {
		t.Fatalf("glob generated media: %v", err)
	}
	if len(assets) != 2 {
		t.Fatalf("generated PNG assets = %v, want formula and Mermaid", assets)
	}
	for _, asset := range assets {
		file, err := os.Open(asset)
		if err != nil {
			t.Errorf("open %s: %v", asset, err)
			continue
		}
		config, err := png.DecodeConfig(file)
		_ = file.Close()
		if err != nil {
			t.Errorf("decode %s: %v", asset, err)
			continue
		}
		if config.Width < 600 || config.Height < 160 {
			t.Errorf("%s dimensions = %dx%d, want high-resolution media", asset, config.Width, config.Height)
		}
	}
}

func TestInspectRejectsUnsafeMermaidAtTheSourceLine(t *testing.T) {
	t.Parallel()

	sourcePath := filepath.Join(t.TempDir(), "unsafe.md")
	if err := os.WriteFile(sourcePath, []byte("# Unsafe\n\n```mermaid\nflowchart LR\nclick A javascript:alert(1)\n```\n"), 0o644); err != nil {
		t.Fatalf("write unsafe article: %v", err)
	}
	inspection, err := builder.New().Inspect(context.Background(), builder.InspectRequest{SourcePath: sourcePath})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if len(inspection.Errors) != 1 || inspection.Errors[0].Code != "MERMAID_UNSAFE" || inspection.Errors[0].Line != 3 {
		t.Errorf("inspection errors = %+v, want MERMAID_UNSAFE at line 3", inspection.Errors)
	}
}

func TestEveryBuiltInThemeRendersTheCompleteGalleryWithoutStructuralLoss(t *testing.T) {
	projectDir := t.TempDir()
	if _, err := workspace.NewLocal().Init(context.Background(), projectDir); err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	listed, err := catalog.NewBuiltin().List(context.Background(), catalog.Query{Kind: catalog.KindTheme})
	if err != nil {
		t.Fatalf("list themes: %v", err)
	}
	hashes := map[string]string{}
	for _, resource := range listed.Resources {
		result, err := builder.New().Build(context.Background(), builder.BuildRequest{
			WorkspaceRoot: projectDir,
			SourcePath:    filepath.Join("..", "..", "testdata", "component-gallery.md"),
			Theme:         resource.Name,
		})
		if err != nil {
			t.Errorf("Build(theme=%s) error = %v", resource.Name, err)
			continue
		}
		articleHTML, err := os.ReadFile(result.ArticleHTMLPath)
		if err != nil {
			t.Errorf("read theme %s HTML: %v", resource.Name, err)
			continue
		}
		if count := strings.Count(string(articleHTML), `data-wx-component="`); count != 24 {
			t.Errorf("theme %s component count = %d, want 24", resource.Name, count)
		}
		hashes[result.ContentHash] = resource.Name
	}
	if len(hashes) != 24 {
		t.Errorf("unique themed content hashes = %d, want 24", len(hashes))
	}
}

func TestBuildDoesNotEmitExecutableArticleMarkup(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	if _, err := workspace.NewLocal().Init(context.Background(), projectDir); err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	sourcePath := filepath.Join(projectDir, "unsafe.md")
	source := []byte("---\ntitle: Safe output\nauthor: Test\n---\n\n# Heading\n\n<script>alert('x')</script>\n\n[unsafe](javascript:alert(1))\n\n:::wx-hero\ntitle: \"<img src=x onerror=alert(1)>\"\nsubtitle: Escaped content\n:::\n")
	if err := os.WriteFile(sourcePath, source, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	result, err := builder.New().Build(context.Background(), builder.BuildRequest{WorkspaceRoot: projectDir, SourcePath: sourcePath})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	articleHTML, err := os.ReadFile(result.ArticleHTMLPath)
	if err != nil {
		t.Fatalf("read HTML: %v", err)
	}
	for _, dangerous := range []string{"<script", `href="javascript:`, `<img src=x`} {
		if strings.Contains(strings.ToLower(string(articleHTML)), dangerous) {
			t.Errorf("article emitted dangerous markup %q:\n%s", dangerous, articleHTML)
		}
	}
	if !bytes.Contains(articleHTML, []byte("&lt;img src=x onerror=alert(1)&gt;")) {
		t.Errorf("component text was not safely escaped:\n%s", articleHTML)
	}
}
