package builder

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wechatloom/wechatloom/internal/catalog"
	"github.com/wechatloom/wechatloom/internal/protocol"
	"github.com/wechatloom/wechatloom/internal/version"
	"github.com/wechatloom/wechatloom/internal/workspace"
	"go.yaml.in/yaml/v3"
)

type Builder interface {
	Inspect(context.Context, InspectRequest) (Inspection, error)
	Build(context.Context, BuildRequest) (BuildResult, error)
}

type InspectRequest struct {
	SourcePath string
}

type Inspection struct {
	SourcePath     string       `json:"source_path"`
	SourceHash     string       `json:"source_hash"`
	Title          string       `json:"title"`
	Author         string       `json:"author"`
	Digest         string       `json:"digest,omitempty"`
	Theme          string       `json:"theme"`
	ComponentCount int          `json:"component_count"`
	CalloutCount   int          `json:"callout_count"`
	Errors         []Diagnostic `json:"errors"`
}

type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
}

type BuildRequest struct {
	WorkspaceRoot string
	SourcePath    string
	Theme         string
}

type BuildResult struct {
	ID                  string       `json:"id"`
	BuildPath           string       `json:"build_path"`
	ArticleHTMLPath     string       `json:"article_html_path"`
	PreviewHTMLPath     string       `json:"preview_html_path"`
	DerivedMarkdownPath string       `json:"derived_markdown_path"`
	ManifestPath        string       `json:"manifest_path"`
	SourceHash          string       `json:"source_hash"`
	ContentHash         string       `json:"content_hash"`
	Warnings            []Diagnostic `json:"warnings"`
}

type Service struct{}

func New() Builder {
	return &Service{}
}

func (service *Service) Inspect(ctx context.Context, request InspectRequest) (Inspection, error) {
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}

	sourcePath, err := filepath.Abs(request.SourcePath)
	if err != nil {
		return Inspection{}, fmt.Errorf("resolve source path: %w", err)
	}
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return Inspection{}, fmt.Errorf("read source: %w", err)
	}

	sum := sha256.Sum256(source)
	metadata, body, metadataError := parseFrontmatter(source)
	inspection := Inspection{
		SourcePath: sourcePath,
		SourceHash: fmt.Sprintf("%x", sum),
		Title:      metadata.Title,
		Author:     metadata.Author,
		Digest:     metadata.Digest,
		Theme:      metadata.Theme,
		Errors:     []Diagnostic{},
	}
	if inspection.Theme == "" {
		inspection.Theme = "minimal"
	}
	if metadataError != nil {
		inspection.Errors = append(inspection.Errors, Diagnostic{
			Code:    "INVALID_FRONTMATTER",
			Message: metadataError.Error(),
			Line:    1,
		})
	}
	if inspection.Title == "" {
		inspection.Title = firstHeading(body)
	}

	componentCount, calloutCount, componentDiagnostics := inspectComponents(ctx, body)
	inspection.ComponentCount = componentCount
	inspection.CalloutCount = calloutCount
	inspection.Errors = append(inspection.Errors, componentDiagnostics...)
	inspection.Errors = append(inspection.Errors, processRichMedia(body).Diagnostics...)
	inspection.Errors = append(inspection.Errors, resolveLocalImages(body, filepath.Dir(sourcePath)).Diagnostics...)
	return inspection, nil
}

func (service *Service) Build(ctx context.Context, request BuildRequest) (BuildResult, error) {
	resolvedWorkspace, err := workspace.NewLocal().Resolve(ctx, request.WorkspaceRoot)
	if err != nil {
		return BuildResult{}, err
	}

	inspection, err := service.Inspect(ctx, InspectRequest{SourcePath: request.SourcePath})
	if err != nil {
		return BuildResult{}, err
	}
	if len(inspection.Errors) != 0 {
		return BuildResult{}, fmt.Errorf("source validation failed: %s", inspection.Errors[0].Message)
	}

	source, err := os.ReadFile(inspection.SourcePath)
	if err != nil {
		return BuildResult{}, fmt.Errorf("read source: %w", err)
	}
	metadata, body, err := parseFrontmatter(source)
	if err != nil {
		return BuildResult{}, err
	}
	if metadata.Theme == "" {
		inspection.Theme = resolvedWorkspace.Config.Build.Theme
	}
	if strings.TrimSpace(request.Theme) != "" {
		inspection.Theme = strings.TrimSpace(request.Theme)
	}
	originalComponentBlocks, _ := scanComponentBlocks(body)
	localImages := resolveLocalImages(body, filepath.Dir(inspection.SourcePath))
	if len(localImages.Diagnostics) != 0 {
		return BuildResult{}, fmt.Errorf("local image validation failed: %s", localImages.Diagnostics[0].Message)
	}
	body = localImages.Markdown
	richMedia := processRichMedia(body)
	if len(richMedia.Diagnostics) != 0 {
		return BuildResult{}, fmt.Errorf("rich media validation failed: %s", richMedia.Diagnostics[0].Message)
	}
	body = richMedia.Markdown
	buildAssets := make(map[string][]byte, len(localImages.Assets)+len(richMedia.Assets))
	for path, content := range localImages.Assets {
		buildAssets[path] = content
	}
	for path, content := range richMedia.Assets {
		buildAssets[path] = content
	}

	resourceCatalog := catalog.NewProject(request.WorkspaceRoot)
	themeDefinition, err := resourceCatalog.Show(ctx, catalog.Ref{
		Kind: catalog.KindTheme,
		Name: inspection.Theme,
	})
	if err != nil {
		return BuildResult{}, fmt.Errorf("resolve theme: %w", err)
	}
	articleHTML, components, err := renderArticle(body, *themeDefinition.Theme)
	if err != nil {
		return BuildResult{}, fmt.Errorf("render article: %w", err)
	}
	contentSum := sha256.Sum256(articleHTML)
	contentHash := fmt.Sprintf("%x", contentSum)
	buildID := time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + contentHash[:12]

	sourceReference, err := json.MarshalIndent(struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	}{
		Path:   inspection.SourcePath,
		SHA256: inspection.SourceHash,
	}, "", "  ")
	if err != nil {
		return BuildResult{}, fmt.Errorf("encode source reference: %w", err)
	}
	sourceReference = append(sourceReference, '\n')

	type layoutInstance struct {
		Order      int    `json:"order"`
		Name       string `json:"name"`
		SourceLine int    `json:"source_line"`
	}
	instances := make([]layoutInstance, 0, len(originalComponentBlocks))
	for index, block := range originalComponentBlocks {
		instances = append(instances, layoutInstance{Order: index + 1, Name: block.Name, SourceLine: block.Line})
	}
	layoutPlan, err := json.MarshalIndent(struct {
		SchemaVersion string           `json:"schema_version"`
		Theme         string           `json:"theme"`
		Components    []string         `json:"components"`
		Instances     []layoutInstance `json:"instances"`
	}{
		SchemaVersion: "1",
		Theme:         inspection.Theme,
		Components:    components,
		Instances:     instances,
	}, "", "  ")
	if err != nil {
		return BuildResult{}, fmt.Errorf("encode layout plan: %w", err)
	}
	layoutPlan = append(layoutPlan, '\n')

	diagnostics := []byte("{\n  \"schema_version\": \"1\",\n  \"warnings\": [],\n  \"errors\": []\n}\n")
	previewHTML, err := renderPreviewPage(ctx, resourceCatalog, inspection.Title, body, inspection.Theme)
	if err != nil {
		return BuildResult{}, fmt.Errorf("render preview: %w", err)
	}

	type artifactRecord struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	}
	artifacts := []artifactRecord{
		{Path: "source.ref.json", SHA256: hashBytes(sourceReference)},
		{Path: "article.derived.md", SHA256: hashBytes(source)},
		{Path: "layout-plan.json", SHA256: hashBytes(layoutPlan)},
		{Path: "article.html", SHA256: contentHash},
		{Path: "preview.html", SHA256: hashBytes(previewHTML)},
		{Path: "diagnostics.json", SHA256: hashBytes(diagnostics)},
	}
	assetPaths := make([]string, 0, len(buildAssets))
	for assetPath := range buildAssets {
		assetPaths = append(assetPaths, assetPath)
	}
	sort.Strings(assetPaths)
	for _, assetPath := range assetPaths {
		artifacts = append(artifacts, artifactRecord{Path: assetPath, SHA256: hashBytes(buildAssets[assetPath])})
	}
	type componentRecord struct {
		Name          string `json:"name"`
		SchemaVersion string `json:"schema_version"`
	}
	componentRecords := make([]componentRecord, 0, len(components))
	seenComponents := make(map[string]bool, len(components))
	for _, component := range components {
		if seenComponents[component] {
			continue
		}
		seenComponents[component] = true
		componentRecords = append(componentRecords, componentRecord{
			Name:          component,
			SchemaVersion: "1",
		})
	}
	manifest := struct {
		SchemaVersion   string `json:"schema_version"`
		ProtocolVersion string `json:"protocol_version"`
		BuildID         string `json:"build_id"`
		Tool            struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"tool"`
		Source struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"source"`
		DerivedMarkdownSHA256 string `json:"derived_markdown_sha256"`
		ContentHash           string `json:"content_hash"`
		Theme                 struct {
			Name    string              `json:"name"`
			Version string              `json:"version"`
			Tokens  catalog.ThemeTokens `json:"tokens"`
		} `json:"theme"`
		Components []componentRecord `json:"components"`
		Render     struct {
			InlineStyles bool   `json:"inline_styles"`
			LinkPolicy   string `json:"link_policy"`
		} `json:"render"`
		Artifacts []artifactRecord `json:"artifacts"`
	}{
		SchemaVersion:         "1",
		ProtocolVersion:       protocol.SchemaVersion,
		BuildID:               buildID,
		DerivedMarkdownSHA256: inspection.SourceHash,
		ContentHash:           contentHash,
		Components:            componentRecords,
		Artifacts:             artifacts,
	}
	manifest.Tool.Name = version.Name
	manifest.Tool.Version = version.Version
	manifest.Source.Path = inspection.SourcePath
	manifest.Source.SHA256 = inspection.SourceHash
	manifest.Theme.Name = inspection.Theme
	manifest.Theme.Version = themeDefinition.Theme.Version
	manifest.Theme.Tokens = themeDefinition.Theme.Tokens
	manifest.Render.InlineStyles = true
	manifest.Render.LinkPolicy = "preserve"

	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return BuildResult{}, fmt.Errorf("encode manifest: %w", err)
	}
	manifestJSON = append(manifestJSON, '\n')

	buildFiles := map[string][]byte{
		"source.ref.json":    sourceReference,
		"article.derived.md": source,
		"layout-plan.json":   layoutPlan,
		"article.html":       articleHTML,
		"preview.html":       previewHTML,
		"manifest.json":      manifestJSON,
		"diagnostics.json":   diagnostics,
	}
	for assetPath, content := range buildAssets {
		buildFiles[assetPath] = content
	}
	committed, err := workspace.NewLocal().CommitBuild(ctx, request.WorkspaceRoot, workspace.BuildRecord{
		ID:          buildID,
		Files:       buildFiles,
		Directories: []string{"assets", "snapshots"},
	})
	if err != nil {
		return BuildResult{}, err
	}

	return BuildResult{
		ID:                  buildID,
		BuildPath:           committed.Path,
		ArticleHTMLPath:     filepath.Join(committed.Path, "article.html"),
		PreviewHTMLPath:     filepath.Join(committed.Path, "preview.html"),
		DerivedMarkdownPath: filepath.Join(committed.Path, "article.derived.md"),
		ManifestPath:        filepath.Join(committed.Path, "manifest.json"),
		SourceHash:          inspection.SourceHash,
		ContentHash:         contentHash,
		Warnings:            []Diagnostic{},
	}, nil
}

func hashBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return fmt.Sprintf("%x", sum)
}

type frontmatter struct {
	Title  string `yaml:"title"`
	Author string `yaml:"author"`
	Digest string `yaml:"digest"`
	Theme  string `yaml:"theme"`
}

func parseFrontmatter(source []byte) (frontmatter, []byte, error) {
	if !bytes.HasPrefix(source, []byte("---\n")) {
		return frontmatter{}, source, nil
	}

	const openingLength = len("---\n")
	end := bytes.Index(source[openingLength:], []byte("\n---\n"))
	if end < 0 {
		return frontmatter{}, source, fmt.Errorf("frontmatter is missing its closing delimiter")
	}

	end += openingLength
	var metadata frontmatter
	if err := yaml.Unmarshal(source[openingLength:end], &metadata); err != nil {
		return frontmatter{}, source[end+len("\n---\n"):], fmt.Errorf("decode frontmatter: %w", err)
	}
	return metadata, source[end+len("\n---\n"):], nil
}

func firstHeading(body []byte) string {
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return ""
}
