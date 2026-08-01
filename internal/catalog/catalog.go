package catalog

import (
	"context"
	"fmt"
)

type Catalog interface {
	Capabilities(context.Context) (Capabilities, error)
	List(context.Context, Query) (Result, error)
	Show(context.Context, Ref) (Definition, error)
	Validate(context.Context, ValidationRequest) (ValidationResult, error)
}

type Kind string

const (
	KindTheme     Kind = "theme"
	KindComponent Kind = "component"
)

type Query struct {
	Kind Kind `json:"kind"`
}

type Result struct {
	Resources []ResourceSummary `json:"resources"`
}

type ResourceSummary struct {
	Kind        Kind   `json:"kind"`
	Name        string `json:"name"`
	Family      string `json:"family,omitempty"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type Ref struct {
	Kind Kind   `json:"kind"`
	Name string `json:"name"`
}

type Definition struct {
	Resource  ResourceSummary      `json:"resource"`
	Theme     *ThemeDefinition     `json:"theme,omitempty"`
	Component *ComponentDefinition `json:"component,omitempty"`
}

type ComponentDefinition struct {
	Name           string            `json:"name"`
	Category       string            `json:"category"`
	Version        string            `json:"version"`
	Description    string            `json:"description"`
	SchemaVersion  string            `json:"schema_version"`
	Fields         []FieldDefinition `json:"fields"`
	ValidExample   string            `json:"valid_example"`
	InvalidExample string            `json:"invalid_example"`
}

type FieldDefinition struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Required    bool              `json:"required"`
	Description string            `json:"description"`
	Enum        []string          `json:"enum,omitempty"`
	Fields      []FieldDefinition `json:"fields,omitempty"`
}

type ValidationRequest struct {
	Ref    Ref            `json:"ref"`
	Fields map[string]any `json:"fields"`
}

type ValidationResult struct {
	Valid      bool        `json:"valid"`
	Violations []Violation `json:"violations"`
}

type Violation struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ThemeDefinition struct {
	Name        string      `json:"name"`
	Family      string      `json:"family"`
	Version     string      `json:"version"`
	Description string      `json:"description"`
	Tokens      ThemeTokens `json:"tokens"`
}

type ThemeTokens struct {
	Colors     ColorTokens      `json:"colors"`
	Typography TypographyTokens `json:"typography"`
	Spacing    SpacingTokens    `json:"spacing"`
	Shape      ShapeTokens      `json:"shape"`
	Code       CodeTokens       `json:"code"`
	Caption    CaptionTokens    `json:"caption"`
}

type ColorTokens struct {
	Background     string `json:"background"`
	Surface        string `json:"surface"`
	Text           string `json:"text"`
	Heading        string `json:"heading"`
	Muted          string `json:"muted"`
	Accent         string `json:"accent"`
	AccentSoft     string `json:"accent_soft"`
	Border         string `json:"border"`
	CodeBackground string `json:"code_background"`
	CodeText       string `json:"code_text"`
}

type TypographyTokens struct {
	FontFamily        string  `json:"font_family"`
	HeadingFontFamily string  `json:"heading_font_family"`
	BaseSize          int     `json:"base_size"`
	LineHeight        float64 `json:"line_height"`
	H1Size            int     `json:"h1_size"`
	H2Size            int     `json:"h2_size"`
	H3Size            int     `json:"h3_size"`
}

type SpacingTokens struct {
	Compact   int `json:"compact"`
	Paragraph int `json:"paragraph"`
	Component int `json:"component"`
	Section   int `json:"section"`
}

type ShapeTokens struct {
	BorderWidth int    `json:"border_width"`
	Radius      int    `json:"radius"`
	ImageRadius int    `json:"image_radius"`
	Shadow      string `json:"shadow"`
}

type CodeTokens struct {
	FontFamily string  `json:"font_family"`
	FontSize   int     `json:"font_size"`
	LineHeight float64 `json:"line_height"`
}

type CaptionTokens struct {
	Color      string  `json:"color"`
	FontSize   int     `json:"font_size"`
	LineHeight float64 `json:"line_height"`
}

type Capabilities struct {
	Themes       []Theme      `json:"themes"`
	Components   []Component  `json:"components"`
	RemoteWrites RemoteWrites `json:"remote_writes"`
}

type Theme struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Component struct {
	Name          string `json:"name"`
	SchemaVersion string `json:"schema_version"`
}

type RemoteWrites struct {
	WeChatDraft bool `json:"wechat_draft"`
}

type Builtin struct{}

var _ Catalog = (*Builtin)(nil)

func NewBuiltin() *Builtin {
	return &Builtin{}
}

func (builtin *Builtin) Capabilities(ctx context.Context) (Capabilities, error) {
	if err := ctx.Err(); err != nil {
		return Capabilities{}, err
	}
	themeResult, err := builtin.List(ctx, Query{Kind: KindTheme})
	if err != nil {
		return Capabilities{}, err
	}
	componentResult, err := builtin.List(ctx, Query{Kind: KindComponent})
	if err != nil {
		return Capabilities{}, err
	}
	themes := make([]Theme, 0, len(themeResult.Resources))
	for _, resource := range themeResult.Resources {
		themes = append(themes, Theme{Name: resource.Name, Version: resource.Version})
	}
	components := make([]Component, 0, len(componentResult.Resources))
	for _, resource := range componentResult.Resources {
		components = append(components, Component{Name: resource.Name, SchemaVersion: "1"})
	}
	return Capabilities{
		Themes:     themes,
		Components: components,
		RemoteWrites: RemoteWrites{
			WeChatDraft: false,
		},
	}, nil
}

func (builtin *Builtin) List(ctx context.Context, query Query) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if query.Kind == KindComponent {
		return Result{Resources: componentSummaries()}, nil
	}
	if query.Kind != KindTheme {
		return Result{Resources: []ResourceSummary{}}, nil
	}

	themes := []ResourceSummary{
		newTheme("minimal", "minimal", "Quiet ink for focused reading"),
		newTheme("minimal-slate", "minimal", "Cool slate for structured tutorials"),
		newTheme("minimal-sand", "minimal", "Warm sand for calm explanations"),
		newTheme("minimal-berry", "minimal", "Berry accent for concise essays"),
		newTheme("editorial-blue", "editorial", "Classic blue for long-form reporting"),
		newTheme("editorial-crimson", "editorial", "Crimson accents for opinion and profiles"),
		newTheme("editorial-olive", "editorial", "Olive tones for thoughtful narratives"),
		newTheme("editorial-sepia", "editorial", "Sepia paper for literary features"),
		newTheme("tech-cyan", "tech", "Cyan signal for AI and engineering"),
		newTheme("tech-violet", "tech", "Violet signal for product stories"),
		newTheme("tech-lime", "tech", "Lime signal for developer guides"),
		newTheme("tech-graphite", "tech", "Graphite contrast for code-heavy articles"),
		newTheme("business-navy", "business", "Navy authority for analysis and reports"),
		newTheme("business-teal", "business", "Teal clarity for services and strategy"),
		newTheme("business-burgundy", "business", "Burgundy depth for premium research"),
		newTheme("business-gold", "business", "Gold restraint for executive summaries"),
		newTheme("culture-cinnabar", "culture", "Cinnabar accents for heritage stories"),
		newTheme("culture-jade", "culture", "Jade accents for arts and tradition"),
		newTheme("culture-indigo", "culture", "Indigo depth for books and humanities"),
		newTheme("culture-parchment", "culture", "Parchment warmth for historical essays"),
		newTheme("lifestyle-coral", "lifestyle", "Coral warmth for travel and wellbeing"),
		newTheme("lifestyle-lavender", "lifestyle", "Lavender softness for reflective stories"),
		newTheme("lifestyle-sage", "lifestyle", "Sage calm for home and nature"),
		newTheme("lifestyle-ocean", "lifestyle", "Ocean freshness for active living"),
	}
	return Result{Resources: themes}, nil
}

func componentSummaries() []ResourceSummary {
	return []ResourceSummary{
		newComponent("hero", "opening", "Article opener with title and subtitle"),
		newComponent("lead", "opening", "Emphasized opening paragraph"),
		newComponent("toc", "opening", "Compact table of contents"),
		newComponent("audience", "opening", "Intended-reader checklist"),
		newComponent("section", "structure", "Section transition heading"),
		newComponent("divider", "structure", "Visual content divider"),
		newComponent("steps", "structure", "Ordered process steps"),
		newComponent("timeline", "structure", "Chronological event sequence"),
		newComponent("checklist", "structure", "Actionable checklist"),
		newComponent("callout", "evidence", "Highlighted notice or reminder"),
		newComponent("quote", "evidence", "Attributed quotation"),
		newComponent("metrics", "evidence", "Key metric tiles"),
		newComponent("compare", "evidence", "Two-sided comparison"),
		newComponent("case", "evidence", "Challenge, solution, and result"),
		newComponent("pros-cons", "evidence", "Balanced benefits and tradeoffs"),
		newComponent("image-text", "media", "Image paired with explanatory text"),
		newComponent("gallery", "media", "Small captioned image gallery"),
		newComponent("code-card", "media", "Framed source-code example"),
		newComponent("data-card", "media", "Label and value data rows"),
		newComponent("summary", "closing", "Closing summary statement"),
		newComponent("takeaways", "closing", "Key takeaway list"),
		newComponent("author", "closing", "Author identity and biography"),
		newComponent("cta", "closing", "Call-to-action block"),
		newComponent("subscribe", "closing", "Subscription reminder"),
	}
}

func newComponent(name, category, description string) ResourceSummary {
	return ResourceSummary{
		Kind:        KindComponent,
		Name:        name,
		Family:      category,
		Version:     "0.2.0",
		Description: description,
	}
}

func (builtin *Builtin) Show(ctx context.Context, ref Ref) (Definition, error) {
	if err := ctx.Err(); err != nil {
		return Definition{}, err
	}
	if ref.Kind == KindComponent {
		result, err := builtin.List(ctx, Query{Kind: KindComponent})
		if err != nil {
			return Definition{}, err
		}
		for _, resource := range result.Resources {
			if resource.Name != ref.Name {
				continue
			}
			component := componentDefinition(resource)
			return Definition{Resource: resource, Component: &component}, nil
		}
		return Definition{}, fmt.Errorf("component %q not found", ref.Name)
	}
	if ref.Kind != KindTheme {
		return Definition{}, fmt.Errorf("unsupported resource kind %q", ref.Kind)
	}

	result, err := builtin.List(ctx, Query{Kind: KindTheme})
	if err != nil {
		return Definition{}, err
	}
	for _, resource := range result.Resources {
		if resource.Name != ref.Name {
			continue
		}
		definition := themeDefinition(resource)
		return Definition{Resource: resource, Theme: &definition}, nil
	}
	return Definition{}, fmt.Errorf("theme %q not found", ref.Name)
}

func newTheme(name, family, description string) ResourceSummary {
	return ResourceSummary{
		Kind:        KindTheme,
		Name:        name,
		Family:      family,
		Version:     "0.2.0",
		Description: description,
	}
}

type palette struct {
	background string
	surface    string
	accent     string
	accentSoft string
	border     string
}

func themeDefinition(resource ResourceSummary) ThemeDefinition {
	selected := themePalettes()[resource.Name]
	fontFamily := `-apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif`
	headingFontFamily := fontFamily
	if resource.Family == "editorial" || resource.Family == "culture" {
		headingFontFamily = `"Iowan Old Style","Songti SC","STSong",serif`
	}

	return ThemeDefinition{
		Name:        resource.Name,
		Family:      resource.Family,
		Version:     resource.Version,
		Description: resource.Description,
		Tokens: ThemeTokens{
			Colors: ColorTokens{
				Background:     selected.background,
				Surface:        selected.surface,
				Text:           "#1F2937",
				Heading:        "#111827",
				Muted:          "#6B7280",
				Accent:         selected.accent,
				AccentSoft:     selected.accentSoft,
				Border:         selected.border,
				CodeBackground: "#0F172A",
				CodeText:       "#E2E8F0",
			},
			Typography: TypographyTokens{
				FontFamily:        fontFamily,
				HeadingFontFamily: headingFontFamily,
				BaseSize:          16,
				LineHeight:        1.8,
				H1Size:            30,
				H2Size:            24,
				H3Size:            19,
			},
			Spacing: SpacingTokens{
				Compact:   8,
				Paragraph: 16,
				Component: 24,
				Section:   40,
			},
			Shape: ShapeTokens{
				BorderWidth: 1,
				Radius:      8,
				ImageRadius: 8,
				Shadow:      "0 6px 24px rgba(15,23,42,0.08)",
			},
			Code: CodeTokens{
				FontFamily: `"SFMono-Regular",Consolas,"Liberation Mono",monospace`,
				FontSize:   14,
				LineHeight: 1.65,
			},
			Caption: CaptionTokens{
				Color:      "#6B7280",
				FontSize:   13,
				LineHeight: 1.6,
			},
		},
	}
}

func themePalettes() map[string]palette {
	return map[string]palette{
		"minimal":            {"#FFFFFF", "#F8FAFC", "#2563EB", "#EFF6FF", "#E2E8F0"},
		"minimal-slate":      {"#F8FAFC", "#F1F5F9", "#475569", "#E2E8F0", "#CBD5E1"},
		"minimal-sand":       {"#FFFBEB", "#FEF3C7", "#B45309", "#FEF3C7", "#FDE68A"},
		"minimal-berry":      {"#FFF7FB", "#FCE7F3", "#BE185D", "#FCE7F3", "#FBCFE8"},
		"editorial-blue":     {"#F8FAFF", "#EFF6FF", "#1D4ED8", "#DBEAFE", "#BFDBFE"},
		"editorial-crimson":  {"#FFF8F8", "#FEF2F2", "#B91C1C", "#FEE2E2", "#FECACA"},
		"editorial-olive":    {"#FAFBF5", "#F7FEE7", "#4D7C0F", "#ECFCCB", "#D9F99D"},
		"editorial-sepia":    {"#FBF7EF", "#FEF3C7", "#92400E", "#FDE68A", "#FCD34D"},
		"tech-cyan":          {"#F5FCFD", "#ECFEFF", "#0E7490", "#CFFAFE", "#A5F3FC"},
		"tech-violet":        {"#FAF8FF", "#F5F3FF", "#7C3AED", "#EDE9FE", "#DDD6FE"},
		"tech-lime":          {"#F9FEEB", "#F7FEE7", "#4D7C0F", "#ECFCCB", "#D9F99D"},
		"tech-graphite":      {"#F7F7F8", "#F1F5F9", "#334155", "#E2E8F0", "#CBD5E1"},
		"business-navy":      {"#F7F9FC", "#EFF6FF", "#1E3A8A", "#DBEAFE", "#BFDBFE"},
		"business-teal":      {"#F5FBFA", "#F0FDFA", "#0F766E", "#CCFBF1", "#99F6E4"},
		"business-burgundy":  {"#FCF7F9", "#FFF1F2", "#9F1239", "#FFE4E6", "#FECDD3"},
		"business-gold":      {"#FFFBF2", "#FFFBEB", "#A16207", "#FEF3C7", "#FDE68A"},
		"culture-cinnabar":   {"#FFF9F5", "#FFF7ED", "#C2410C", "#FFEDD5", "#FED7AA"},
		"culture-jade":       {"#F5FBF8", "#ECFDF5", "#047857", "#D1FAE5", "#A7F3D0"},
		"culture-indigo":     {"#F8F7FC", "#EEF2FF", "#4338CA", "#E0E7FF", "#C7D2FE"},
		"culture-parchment":  {"#FCF8EE", "#FEF3C7", "#854D0E", "#FDE68A", "#FCD34D"},
		"lifestyle-coral":    {"#FFF8F6", "#FFF1F2", "#E11D48", "#FFE4E6", "#FECDD3"},
		"lifestyle-lavender": {"#FBF8FF", "#FAF5FF", "#7E22CE", "#F3E8FF", "#E9D5FF"},
		"lifestyle-sage":     {"#F7FBF6", "#F7FEE7", "#3F6212", "#ECFCCB", "#D9F99D"},
		"lifestyle-ocean":    {"#F4FAFF", "#F0F9FF", "#0369A1", "#E0F2FE", "#BAE6FD"},
	}
}
