package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type ThemePackage struct {
	SchemaVersion string          `json:"schema_version"`
	Theme         ThemeDefinition `json:"theme"`
}

var resourceNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
var hexColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

func ValidateThemePackage(packageFile ThemePackage) ValidationResult {
	violations := make([]Violation, 0)
	add := func(field, code, message string) {
		violations = append(violations, Violation{Field: field, Code: code, Message: message})
	}
	if packageFile.SchemaVersion != "1" {
		add("schema_version", "SCHEMA_VERSION", `schema_version must be "1"`)
	}
	theme := packageFile.Theme
	if !resourceNamePattern.MatchString(theme.Name) {
		add("theme.name", "NAME", "theme.name must use lowercase letters, numbers, and hyphens")
	}
	if !contains([]string{"minimal", "editorial", "tech", "business", "culture", "lifestyle", "custom"}, theme.Family) {
		add("theme.family", "FAMILY", "theme.family is unsupported")
	}
	if !versionPattern.MatchString(theme.Version) {
		add("theme.version", "VERSION", "theme.version must be a semantic version")
	}
	if strings.TrimSpace(theme.Description) == "" {
		add("theme.description", "REQUIRED", "theme.description is required")
	}
	colors := map[string]string{
		"background": theme.Tokens.Colors.Background, "surface": theme.Tokens.Colors.Surface,
		"text": theme.Tokens.Colors.Text, "heading": theme.Tokens.Colors.Heading,
		"muted": theme.Tokens.Colors.Muted, "accent": theme.Tokens.Colors.Accent,
		"accent_soft": theme.Tokens.Colors.AccentSoft, "border": theme.Tokens.Colors.Border,
		"code_background": theme.Tokens.Colors.CodeBackground, "code_text": theme.Tokens.Colors.CodeText,
	}
	colorNames := make([]string, 0, len(colors))
	for name := range colors {
		colorNames = append(colorNames, name)
	}
	sort.Strings(colorNames)
	for _, name := range colorNames {
		if !hexColorPattern.MatchString(colors[name]) {
			add("theme.tokens.colors."+name, "COLOR", "color must use six-digit hexadecimal notation")
		}
	}
	fontStacks := []struct{ field, value string }{
		{"font_family", theme.Tokens.Typography.FontFamily},
		{"heading_font_family", theme.Tokens.Typography.HeadingFontFamily},
		{"code.font_family", theme.Tokens.Code.FontFamily},
	}
	for _, item := range fontStacks {
		field, fontStack := item.field, item.value
		lower := strings.ToLower(fontStack)
		if strings.TrimSpace(fontStack) == "" {
			add("theme.tokens.typography."+field, "REQUIRED", "font stack is required")
		} else if strings.Contains(lower, "http") || strings.Contains(lower, "url(") || strings.Contains(lower, "@import") {
			add("theme.tokens.typography."+field, "REMOTE_FONT", "remote fonts are not allowed")
		} else if strings.ContainsAny(fontStack, ";<>{}()\\") {
			add("theme.tokens.typography."+field, "CSS_UNSAFE", "font stack contains unsafe CSS characters")
		}
	}
	shadow := strings.ToLower(theme.Tokens.Shape.Shadow)
	if strings.ContainsAny(shadow, ";<>\"'") || strings.Contains(shadow, "url(") || strings.Contains(shadow, "expression(") {
		add("theme.tokens.shape.shadow", "CSS_UNSAFE", "shadow contains unsafe CSS")
	}
	if theme.Tokens.Typography.BaseSize < 12 || theme.Tokens.Typography.BaseSize > 24 ||
		theme.Tokens.Typography.LineHeight < 1.4 || theme.Tokens.Typography.LineHeight > 2.4 ||
		theme.Tokens.Typography.H1Size <= theme.Tokens.Typography.H2Size ||
		theme.Tokens.Typography.H2Size <= theme.Tokens.Typography.H3Size {
		add("theme.tokens.typography", "TYPOGRAPHY", "typography sizes or line height are outside safe mobile ranges")
	}
	if theme.Tokens.Spacing.Compact <= 0 || theme.Tokens.Spacing.Paragraph <= 0 || theme.Tokens.Spacing.Component <= 0 || theme.Tokens.Spacing.Section <= 0 {
		add("theme.tokens.spacing", "SPACING", "spacing values must be positive")
	}
	if theme.Tokens.Shape.BorderWidth < 0 || theme.Tokens.Shape.Radius < 0 || theme.Tokens.Shape.ImageRadius < 0 {
		add("theme.tokens.shape", "SHAPE", "shape values cannot be negative")
	}
	if hexColorPattern.MatchString(theme.Tokens.Colors.Text) && hexColorPattern.MatchString(theme.Tokens.Colors.Background) {
		if contrast(theme.Tokens.Colors.Text, theme.Tokens.Colors.Background) < 4.5 {
			add("theme.tokens.colors.text", "CONTRAST", "text/background contrast must be at least 4.5:1")
		}
	}
	if hexColorPattern.MatchString(theme.Tokens.Colors.Heading) && hexColorPattern.MatchString(theme.Tokens.Colors.Background) {
		if contrast(theme.Tokens.Colors.Heading, theme.Tokens.Colors.Background) < 4.5 {
			add("theme.tokens.colors.heading", "CONTRAST", "heading/background contrast must be at least 4.5:1")
		}
	}
	return ValidationResult{Valid: len(violations) == 0, Violations: violations}
}

func DecodeThemePackage(path string) (ThemePackage, error) {
	file, err := os.Open(path)
	if err != nil {
		return ThemePackage{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var packageFile ThemePackage
	if err := decoder.Decode(&packageFile); err != nil {
		return ThemePackage{}, fmt.Errorf("decode theme package: %w", err)
	}
	validation := ValidateThemePackage(packageFile)
	if !validation.Valid {
		return ThemePackage{}, fmt.Errorf("invalid theme package: %s", validation.Violations[0].Message)
	}
	return packageFile, nil
}

type ProjectCatalog struct {
	root    string
	builtin *Builtin
}

var _ Catalog = (*ProjectCatalog)(nil)

func NewProject(root string) *ProjectCatalog {
	absolute, err := filepath.Abs(root)
	if err != nil {
		absolute = root
	}
	return &ProjectCatalog{root: absolute, builtin: NewBuiltin()}
}

func (project *ProjectCatalog) Capabilities(ctx context.Context) (Capabilities, error) {
	themes, err := project.List(ctx, Query{Kind: KindTheme})
	if err != nil {
		return Capabilities{}, err
	}
	components, err := project.List(ctx, Query{Kind: KindComponent})
	if err != nil {
		return Capabilities{}, err
	}
	result := Capabilities{RemoteWrites: RemoteWrites{WeChatDraft: false}}
	for _, resource := range themes.Resources {
		result.Themes = append(result.Themes, Theme{Name: resource.Name, Version: resource.Version})
	}
	for _, resource := range components.Resources {
		result.Components = append(result.Components, Component{Name: resource.Name, SchemaVersion: "1"})
	}
	return result, nil
}

func (project *ProjectCatalog) List(ctx context.Context, query Query) (Result, error) {
	base, err := project.builtin.List(ctx, query)
	if err != nil || query.Kind != KindTheme {
		return base, err
	}
	packages, err := project.packages()
	if err != nil {
		return Result{}, err
	}
	indexes := make(map[string]int, len(base.Resources))
	for index, resource := range base.Resources {
		indexes[resource.Name] = index
	}
	customNames := make([]string, 0)
	for name, packageFile := range packages {
		resource := ResourceSummary{Kind: KindTheme, Name: name, Family: packageFile.Theme.Family, Version: packageFile.Theme.Version, Description: packageFile.Theme.Description}
		if index, exists := indexes[name]; exists {
			base.Resources[index] = resource
		} else {
			customNames = append(customNames, name)
		}
	}
	sort.Strings(customNames)
	for _, name := range customNames {
		packageFile := packages[name]
		base.Resources = append(base.Resources, ResourceSummary{Kind: KindTheme, Name: name, Family: packageFile.Theme.Family, Version: packageFile.Theme.Version, Description: packageFile.Theme.Description})
	}
	return base, nil
}

func (project *ProjectCatalog) Show(ctx context.Context, ref Ref) (Definition, error) {
	if err := ctx.Err(); err != nil {
		return Definition{}, err
	}
	if ref.Kind != KindTheme {
		return project.builtin.Show(ctx, ref)
	}
	packages, err := project.packages()
	if err != nil {
		return Definition{}, err
	}
	if packageFile, exists := packages[ref.Name]; exists {
		resource := ResourceSummary{Kind: KindTheme, Name: ref.Name, Family: packageFile.Theme.Family, Version: packageFile.Theme.Version, Description: packageFile.Theme.Description}
		theme := packageFile.Theme
		return Definition{Resource: resource, Theme: &theme}, nil
	}
	return project.builtin.Show(ctx, ref)
}

func (project *ProjectCatalog) Validate(ctx context.Context, request ValidationRequest) (ValidationResult, error) {
	return project.builtin.Validate(ctx, request)
}

func (project *ProjectCatalog) packages() (map[string]ThemePackage, error) {
	root := filepath.Join(project.root, ".wechatloom", "themes")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return map[string]ThemePackage{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read project themes: %w", err)
	}
	packages := make(map[string]ThemePackage)
	for _, entry := range entries {
		if !entry.IsDir() || !resourceNamePattern.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(root, entry.Name(), "theme.json")
		packageFile, err := DecodeThemePackage(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("load project theme %q: %w", entry.Name(), err)
		}
		if packageFile.Theme.Name != entry.Name() {
			return nil, fmt.Errorf("project theme directory %q does not match package name %q", entry.Name(), packageFile.Theme.Name)
		}
		packages[entry.Name()] = packageFile
	}
	return packages, nil
}

func contrast(foreground, background string) float64 {
	left := luminance(foreground)
	right := luminance(background)
	return (math.Max(left, right) + 0.05) / (math.Min(left, right) + 0.05)
}

func luminance(value string) float64 {
	channels := make([]float64, 3)
	for index := 0; index < 3; index++ {
		parsed, _ := strconv.ParseUint(value[1+index*2:3+index*2], 16, 8)
		channel := float64(parsed) / 255
		if channel <= 0.04045 {
			channels[index] = channel / 12.92
		} else {
			channels[index] = math.Pow((channel+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2]
}
