package catalog_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/wechatloom/wechatloom/internal/catalog"
	"go.yaml.in/yaml/v3"
)

func TestCatalogListsSixCompleteThemeFamilies(t *testing.T) {
	t.Parallel()

	result, err := catalog.NewBuiltin().List(context.Background(), catalog.Query{
		Kind: catalog.KindTheme,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	wantNames := []string{
		"business-burgundy", "business-gold", "business-navy", "business-teal",
		"culture-cinnabar", "culture-indigo", "culture-jade", "culture-parchment",
		"editorial-blue", "editorial-crimson", "editorial-olive", "editorial-sepia",
		"lifestyle-coral", "lifestyle-lavender", "lifestyle-ocean", "lifestyle-sage",
		"minimal", "minimal-berry", "minimal-sand", "minimal-slate",
		"tech-cyan", "tech-graphite", "tech-lime", "tech-violet",
	}
	gotNames := make([]string, 0, len(result.Resources))
	familyCounts := map[string]int{}
	for _, resource := range result.Resources {
		gotNames = append(gotNames, resource.Name)
		familyCounts[resource.Family]++
	}
	sort.Strings(gotNames)

	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Errorf("theme names = %v, want %v", gotNames, wantNames)
	}
	if !reflect.DeepEqual(familyCounts, map[string]int{
		"minimal": 4, "editorial": 4, "tech": 4,
		"business": 4, "culture": 4, "lifestyle": 4,
	}) {
		t.Errorf("family counts = %v, want six families with four themes each", familyCounts)
	}
}

func TestEveryThemeHasCompleteReadableDesignTokens(t *testing.T) {
	t.Parallel()

	builtin := catalog.NewBuiltin()
	result, err := builtin.List(context.Background(), catalog.Query{Kind: catalog.KindTheme})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	palettes := map[string]bool{}
	for _, resource := range result.Resources {
		definition, err := builtin.Show(context.Background(), catalog.Ref{
			Kind: catalog.KindTheme,
			Name: resource.Name,
		})
		if err != nil {
			t.Errorf("Show(%q) error = %v", resource.Name, err)
			continue
		}
		if definition.Theme == nil {
			t.Errorf("Show(%q) returned no theme definition", resource.Name)
			continue
		}

		tokens := definition.Theme.Tokens
		if tokens.Colors.Background == "" || tokens.Colors.Surface == "" ||
			tokens.Colors.Text == "" || tokens.Colors.Heading == "" ||
			tokens.Colors.Muted == "" || tokens.Colors.Accent == "" ||
			tokens.Colors.AccentSoft == "" || tokens.Colors.Border == "" ||
			tokens.Colors.CodeBackground == "" || tokens.Colors.CodeText == "" {
			t.Errorf("theme %q has incomplete color tokens: %+v", resource.Name, tokens.Colors)
		}
		if tokens.Typography.FontFamily == "" || tokens.Typography.HeadingFontFamily == "" ||
			tokens.Typography.BaseSize <= 0 || tokens.Typography.LineHeight < 1.5 ||
			tokens.Typography.H1Size <= tokens.Typography.H2Size ||
			tokens.Typography.H2Size <= tokens.Typography.H3Size {
			t.Errorf("theme %q has incomplete typography tokens: %+v", resource.Name, tokens.Typography)
		}
		if strings.Contains(tokens.Typography.FontFamily, "http") ||
			strings.Contains(tokens.Typography.HeadingFontFamily, "http") {
			t.Errorf("theme %q references a remote font", resource.Name)
		}
		if tokens.Spacing.Paragraph <= 0 || tokens.Spacing.Component <= 0 ||
			tokens.Spacing.Section <= tokens.Spacing.Component ||
			tokens.Shape.Radius < 0 || tokens.Shape.ImageRadius < 0 ||
			tokens.Code.FontFamily == "" || tokens.Code.FontSize <= 0 ||
			tokens.Caption.FontSize <= 0 || tokens.Caption.Color == "" {
			t.Errorf("theme %q has incomplete spacing/shape/content tokens: %+v", resource.Name, tokens)
		}

		textContrast, err := contrastRatio(tokens.Colors.Text, tokens.Colors.Background)
		if err != nil {
			t.Errorf("theme %q text contrast: %v", resource.Name, err)
		} else if textContrast < 4.5 {
			t.Errorf("theme %q text contrast = %.2f, want at least 4.5", resource.Name, textContrast)
		}
		headingContrast, err := contrastRatio(tokens.Colors.Heading, tokens.Colors.Background)
		if err != nil {
			t.Errorf("theme %q heading contrast: %v", resource.Name, err)
		} else if headingContrast < 4.5 {
			t.Errorf("theme %q heading contrast = %.2f, want at least 4.5", resource.Name, headingContrast)
		}
		palettes[tokens.Colors.Background+"/"+tokens.Colors.Accent] = true
	}

	if len(palettes) != 24 {
		t.Errorf("unique background/accent palettes = %d, want 24", len(palettes))
	}
}

func TestCatalogListsTwentyFourComponentsInStableCategories(t *testing.T) {
	t.Parallel()

	result, err := catalog.NewBuiltin().List(context.Background(), catalog.Query{
		Kind: catalog.KindComponent,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	wantNames := []string{
		"audience", "author", "callout", "case", "checklist", "code-card",
		"compare", "cta", "data-card", "divider", "gallery", "hero",
		"image-text", "lead", "metrics", "pros-cons", "quote", "section",
		"steps", "subscribe", "summary", "takeaways", "timeline", "toc",
	}
	gotNames := make([]string, 0, len(result.Resources))
	categoryCounts := map[string]int{}
	for _, resource := range result.Resources {
		gotNames = append(gotNames, resource.Name)
		categoryCounts[resource.Family]++
	}
	sort.Strings(gotNames)

	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Errorf("component names = %v, want %v", gotNames, wantNames)
	}
	if !reflect.DeepEqual(categoryCounts, map[string]int{
		"opening": 4, "structure": 5, "evidence": 6, "media": 4, "closing": 5,
	}) {
		t.Errorf("category counts = %v, want stable 4/5/6/4/5 grouping", categoryCounts)
	}
}

func TestEveryComponentExposesSchemaAndExamples(t *testing.T) {
	t.Parallel()

	builtin := catalog.NewBuiltin()
	result, err := builtin.List(context.Background(), catalog.Query{Kind: catalog.KindComponent})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	for _, resource := range result.Resources {
		definition, err := builtin.Show(context.Background(), catalog.Ref{
			Kind: catalog.KindComponent,
			Name: resource.Name,
		})
		if err != nil {
			t.Errorf("Show(%q) error = %v", resource.Name, err)
			continue
		}
		if definition.Component == nil {
			t.Errorf("Show(%q) returned no component definition", resource.Name)
			continue
		}

		component := definition.Component
		if component.SchemaVersion != "1" {
			t.Errorf("component %q schema version = %q, want 1", resource.Name, component.SchemaVersion)
		}
		if len(component.Fields) == 0 {
			t.Errorf("component %q has no fields", resource.Name)
		}
		hasRequired := false
		for _, field := range component.Fields {
			if field.Name == "" || field.Type == "" || field.Description == "" {
				t.Errorf("component %q has incomplete field definition: %+v", resource.Name, field)
			}
			hasRequired = hasRequired || field.Required
		}
		if !hasRequired {
			t.Errorf("component %q has no required field", resource.Name)
		}
		opening := ":::wx-" + resource.Name + "\n"
		if !strings.HasPrefix(component.ValidExample, opening) ||
			!strings.HasSuffix(component.ValidExample, ":::\n") {
			t.Errorf("component %q valid example is not a complete directive:\n%s", resource.Name, component.ValidExample)
		}
		if !strings.HasPrefix(component.InvalidExample, opening) ||
			!strings.HasSuffix(component.InvalidExample, ":::\n") ||
			component.InvalidExample == component.ValidExample {
			t.Errorf("component %q invalid example is missing or not distinct", resource.Name)
		}
		validFields := decodeExampleFields(t, component.ValidExample)
		validResult, err := builtin.Validate(context.Background(), catalog.ValidationRequest{Ref: catalog.Ref{Kind: catalog.KindComponent, Name: resource.Name}, Fields: validFields})
		if err != nil || !validResult.Valid {
			t.Errorf("component %q valid example validation = %+v, err = %v", resource.Name, validResult, err)
		}
		invalidFields := decodeExampleFields(t, component.InvalidExample)
		invalidResult, err := builtin.Validate(context.Background(), catalog.ValidationRequest{Ref: catalog.Ref{Kind: catalog.KindComponent, Name: resource.Name}, Fields: invalidFields})
		if err != nil || invalidResult.Valid {
			t.Errorf("component %q invalid example validation = %+v, err = %v", resource.Name, invalidResult, err)
		}
	}
}

func decodeExampleFields(t *testing.T, example string) map[string]any {
	t.Helper()
	lines := strings.Split(example, "\n")
	if len(lines) < 3 {
		t.Fatalf("invalid component example: %q", example)
	}
	fields := map[string]any{}
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:len(lines)-2], "\n")), &fields); err != nil {
		t.Fatalf("decode component example: %v", err)
	}
	return fields
}

func TestCatalogValidatesComponentFieldsAgainstThePublishedSchema(t *testing.T) {
	t.Parallel()

	builtin := catalog.NewBuiltin()
	valid, err := builtin.Validate(context.Background(), catalog.ValidationRequest{
		Ref: catalog.Ref{Kind: catalog.KindComponent, Name: "timeline"},
		Fields: map[string]any{
			"title": "发布流程",
			"items": []any{
				map[string]any{"time": "第一步", "title": "构建", "text": "生成 HTML"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Validate(valid) error = %v", err)
	}
	if !valid.Valid || len(valid.Violations) != 0 {
		t.Errorf("Validate(valid) = %+v, want valid", valid)
	}

	invalid, err := builtin.Validate(context.Background(), catalog.ValidationRequest{
		Ref: catalog.Ref{Kind: catalog.KindComponent, Name: "callout"},
		Fields: map[string]any{
			"tone":    "urgent",
			"content": "缺少标题并使用了未知 tone。",
			"extra":   "not allowed",
		},
	})
	if err != nil {
		t.Fatalf("Validate(invalid) error = %v", err)
	}
	if invalid.Valid || len(invalid.Violations) != 3 {
		t.Fatalf("Validate(invalid) = %+v, want three violations", invalid)
	}
	wantCodes := []string{"REQUIRED", "ENUM", "UNKNOWN_FIELD"}
	gotCodes := make([]string, 0, len(invalid.Violations))
	for _, violation := range invalid.Violations {
		gotCodes = append(gotCodes, violation.Code)
	}
	if !reflect.DeepEqual(gotCodes, wantCodes) {
		t.Errorf("violation codes = %v, want %v", gotCodes, wantCodes)
	}
}

func TestThemePackagesAreDataOnlyAndProjectThemesOverrideBuiltins(t *testing.T) {
	t.Parallel()

	definition, err := catalog.NewBuiltin().Show(context.Background(), catalog.Ref{Kind: catalog.KindTheme, Name: "minimal"})
	if err != nil {
		t.Fatalf("Show(minimal) error = %v", err)
	}
	packageFile := catalog.ThemePackage{SchemaVersion: "1", Theme: *definition.Theme}
	validation := catalog.ValidateThemePackage(packageFile)
	if !validation.Valid {
		t.Fatalf("built-in package validation = %+v, want valid", validation)
	}
	unsafePackage := packageFile
	unsafePackage.Theme.Name = "custom-minimal"
	unsafePackage.Theme.Tokens.Typography.FontFamily = "url(https://fonts.example/font.woff2)"
	unsafePackage.Theme.Tokens.Colors.Text = "not-a-color"
	unsafePackage.Theme.Tokens.Shape.Shadow = `0 0; background:url(https://example.com/x)`
	unsafeValidation := catalog.ValidateThemePackage(unsafePackage)
	if unsafeValidation.Valid || len(unsafeValidation.Violations) < 2 {
		t.Errorf("unsafe package validation = %+v, want font and color violations", unsafeValidation)
	}

	projectRoot := t.TempDir()
	packageFile.Theme.Version = "0.2.1-project"
	encoded, err := json.Marshal(packageFile)
	if err != nil {
		t.Fatalf("encode theme package: %v", err)
	}
	themeDirectory := filepath.Join(projectRoot, ".wechatloom", "themes", "minimal")
	if err := os.MkdirAll(themeDirectory, 0o755); err != nil {
		t.Fatalf("create theme directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(themeDirectory, "theme.json"), encoded, 0o644); err != nil {
		t.Fatalf("write project theme: %v", err)
	}
	projectCatalog := catalog.NewProject(projectRoot)
	projectDefinition, err := projectCatalog.Show(context.Background(), catalog.Ref{Kind: catalog.KindTheme, Name: "minimal"})
	if err != nil {
		t.Fatalf("project Show(minimal) error = %v", err)
	}
	if projectDefinition.Theme.Version != "0.2.1-project" {
		t.Errorf("project theme version = %q, want project override", projectDefinition.Theme.Version)
	}
}

func TestCatalogRejectsNonHTTPSCallToActionURLs(t *testing.T) {
	t.Parallel()

	result, err := catalog.NewBuiltin().Validate(context.Background(), catalog.ValidationRequest{
		Ref: catalog.Ref{Kind: catalog.KindComponent, Name: "cta"},
		Fields: map[string]any{
			"title": "Unsafe", "text": "Do not run", "button_label": "Open", "url": "javascript:alert(1)",
		},
	})
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if result.Valid || len(result.Violations) != 1 || result.Violations[0].Code != "URL" {
		t.Errorf("Validate() = %+v, want URL violation", result)
	}
}

func TestCommittedPortableAssetsMatchTheRuntimeCatalog(t *testing.T) {
	t.Parallel()

	builtin := catalog.NewBuiltin()
	themes, err := builtin.List(context.Background(), catalog.Query{Kind: catalog.KindTheme})
	if err != nil {
		t.Fatalf("list themes: %v", err)
	}
	for _, resource := range themes.Resources {
		runtimeDefinition, err := builtin.Show(context.Background(), catalog.Ref{Kind: catalog.KindTheme, Name: resource.Name})
		if err != nil {
			t.Errorf("show theme %s: %v", resource.Name, err)
			continue
		}
		packagePath := filepath.Join("..", "..", "themes", resource.Name, "theme.json")
		packageFile, err := catalog.DecodeThemePackage(packagePath)
		if err != nil {
			t.Errorf("decode committed theme %s: %v", resource.Name, err)
			continue
		}
		if !reflect.DeepEqual(packageFile.Theme, *runtimeDefinition.Theme) {
			t.Errorf("committed theme %s differs from runtime catalog", resource.Name)
		}
	}

	components, err := builtin.List(context.Background(), catalog.Query{Kind: catalog.KindComponent})
	if err != nil {
		t.Fatalf("list components: %v", err)
	}
	for _, resource := range components.Resources {
		definition, err := builtin.Show(context.Background(), catalog.Ref{Kind: catalog.KindComponent, Name: resource.Name})
		if err != nil {
			t.Errorf("show component %s: %v", resource.Name, err)
			continue
		}
		schemaPath := filepath.Join("..", "..", "components", resource.Name, "schema.json")
		schemaBytes, err := os.ReadFile(schemaPath)
		if err != nil {
			t.Errorf("read component schema %s: %v", resource.Name, err)
			continue
		}
		var committed catalog.ComponentSchema
		if err := json.Unmarshal(schemaBytes, &committed); err != nil {
			t.Errorf("decode component schema %s: %v", resource.Name, err)
			continue
		}
		if !reflect.DeepEqual(committed, catalog.ComponentJSONSchema(*definition.Component)) {
			t.Errorf("committed component schema %s differs from runtime catalog", resource.Name)
		}
		for fileName, want := range map[string]string{"valid.md": definition.Component.ValidExample, "invalid.md": definition.Component.InvalidExample} {
			content, err := os.ReadFile(filepath.Join("..", "..", "components", resource.Name, "examples", fileName))
			if err != nil {
				t.Errorf("read component %s %s: %v", resource.Name, fileName, err)
				continue
			}
			if string(content) != want {
				t.Errorf("component %s %s differs from runtime catalog", resource.Name, fileName)
			}
		}
	}
}

func contrastRatio(foreground, background string) (float64, error) {
	foregroundLuminance, err := relativeLuminance(foreground)
	if err != nil {
		return 0, err
	}
	backgroundLuminance, err := relativeLuminance(background)
	if err != nil {
		return 0, err
	}
	lighter := math.Max(foregroundLuminance, backgroundLuminance)
	darker := math.Min(foregroundLuminance, backgroundLuminance)
	return (lighter + 0.05) / (darker + 0.05), nil
}

func relativeLuminance(color string) (float64, error) {
	if len(color) != 7 || color[0] != '#' {
		return 0, fmt.Errorf("invalid hex color %q", color)
	}
	channels := make([]float64, 3)
	for index := 0; index < 3; index++ {
		value, err := strconv.ParseUint(color[1+index*2:3+index*2], 16, 8)
		if err != nil {
			return 0, fmt.Errorf("invalid hex color %q: %w", color, err)
		}
		channel := float64(value) / 255
		if channel <= 0.04045 {
			channels[index] = channel / 12.92
		} else {
			channels[index] = math.Pow((channel+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2], nil
}
