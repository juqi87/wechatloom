package builder

import (
	"bytes"
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/wechatloom/wechatloom/internal/catalog"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

func renderArticle(body []byte, theme catalog.ThemeDefinition) ([]byte, []string, error) {
	segments, components, err := splitComponents(body)
	if err != nil {
		return nil, nil, err
	}
	markdown := goldmark.New(goldmark.WithExtensions(extension.GFM))
	tokens := theme.Tokens
	var output bytes.Buffer
	fmt.Fprintf(&output,
		"<section data-wechatloom-theme=\"%s\" style=\"background:%s;box-sizing:border-box;color:%s;font-family:%s;font-size:%dpx;line-height:%.2f;max-width:100%%;overflow-wrap:anywhere;padding:1px 0;word-break:break-word;\">\n",
		escape(theme.Name), tokens.Colors.Background, tokens.Colors.Text,
		escape(tokens.Typography.FontFamily), tokens.Typography.BaseSize, tokens.Typography.LineHeight,
	)
	for _, segment := range segments {
		if segment.Component != nil {
			writeComponent(&output, *segment.Component, tokens)
			continue
		}
		var rendered bytes.Buffer
		if err := markdown.Convert(segment.Markdown, &rendered); err != nil {
			return nil, nil, err
		}
		output.WriteString(inlineStyles(rendered.String(), tokens))
	}
	output.WriteString("</section>\n")
	return output.Bytes(), components, nil
}

func writeComponent(out *bytes.Buffer, component componentBlock, tokens catalog.ThemeTokens) {
	switch component.Name {
	case "hero":
		open(out, "header", component.Name, fmt.Sprintf("background:%s;border-bottom:4px solid %s;padding:28px 22px;", tokens.Colors.AccentSoft, tokens.Colors.Accent))
		label(out, text(component, "eyebrow"), tokens)
		heading(out, "h1", text(component, "title"), tokens.Typography.H1Size, tokens)
		paragraph(out, text(component, "subtitle"), "font-size:18px;margin:8px 0 0;", tokens)
		out.WriteString("</header>\n")
	case "lead":
		open(out, "section", component.Name, fmt.Sprintf("border-left:5px solid %s;margin:24px 0;padding:4px 12px;", tokens.Colors.Accent))
		paragraph(out, text(component, "text"), "font-size:16px;font-weight:600;margin:0;", tokens)
		out.WriteString("</section>\n")
	case "toc", "audience", "checklist", "takeaways":
		openCard(out, component.Name, tokens)
		componentTitle(out, text(component, "title"), tokens)
		writeList(out, stringsFor(component, "items"), component.Name == "checklist", false, tokens)
		out.WriteString("</section>\n")
	case "section":
		open(out, "header", component.Name, fmt.Sprintf("border-bottom:2px solid %s;margin:40px 0 20px;padding-bottom:10px;", tokens.Colors.Border))
		label(out, text(component, "kicker"), tokens)
		heading(out, "h2", text(component, "title"), tokens.Typography.H2Size, tokens)
		out.WriteString("</header>\n")
	case "divider":
		open(out, "div", component.Name, fmt.Sprintf("border-top:1px solid %s;margin:36px 0;text-align:center;", tokens.Colors.Border))
		fmt.Fprintf(out, "<span style=\"background:%s;color:%s;display:inline-block;font-size:13px;padding:0 12px;transform:translateY(-14px);\">%s</span>\n", tokens.Colors.Background, tokens.Colors.Muted, escape(text(component, "label")))
		out.WriteString("</div>\n")
	case "steps":
		openCard(out, component.Name, tokens)
		componentTitle(out, text(component, "title"), tokens)
		writeList(out, stringsFor(component, "items"), false, true, tokens)
		out.WriteString("</section>\n")
	case "timeline":
		openCard(out, component.Name, tokens)
		componentTitle(out, text(component, "title"), tokens)
		for _, item := range objectsFor(component, "items") {
			fmt.Fprintf(out, "<div style=\"border-left:3px solid %s;padding:0 0 18px 16px;\"><span style=\"color:%s;font-size:13px;font-weight:700;\">%s</span><p style=\"color:%s;font-weight:700;margin:2px 0;\">%s</p><p style=\"margin:0;\">%s</p></div>\n", tokens.Colors.Accent, tokens.Colors.Accent, escape(stringValue(item, "time")), tokens.Colors.Heading, escape(stringValue(item, "title")), escape(stringValue(item, "text")))
		}
		out.WriteString("</section>\n")
	case "callout":
		writeCallout(out, component, tokens)
	case "quote":
		open(out, "blockquote", component.Name, fmt.Sprintf("border-left:5px solid %s;margin:24px 0;padding:8px 20px;", tokens.Colors.Accent))
		paragraph(out, "“"+text(component, "text")+"”", "font-size:20px;font-weight:600;margin:0 0 8px;", tokens)
		fmt.Fprintf(out, "<footer style=\"color:%s;font-size:13px;\">— %s</footer>\n", tokens.Colors.Muted, escape(text(component, "attribution")))
		out.WriteString("</blockquote>\n")
	case "metrics":
		openCard(out, component.Name, tokens)
		componentTitle(out, text(component, "title"), tokens)
		out.WriteString("<table role=\"presentation\" style=\"border-collapse:collapse;width:100%;\"><tr>\n")
		for _, item := range objectsFor(component, "items") {
			fmt.Fprintf(out, "<td style=\"border:0;padding:12px;text-align:center;\"><strong style=\"color:%s;display:block;font-size:28px;\">%s</strong><span style=\"color:%s;font-size:13px;\">%s</span></td>\n", tokens.Colors.Accent, escape(stringValue(item, "value")), tokens.Colors.Muted, escape(stringValue(item, "label")))
		}
		out.WriteString("</tr></table></section>\n")
	case "compare", "pros-cons":
		writeComparison(out, component, tokens)
	case "case":
		openCard(out, component.Name, tokens)
		componentTitle(out, text(component, "title"), tokens)
		for _, row := range []struct{ label, field string }{{"挑战", "challenge"}, {"方案", "solution"}, {"结果", "result"}} {
			fmt.Fprintf(out, "<p style=\"margin:10px 0;\"><strong style=\"color:%s;\">%s：</strong>%s</p>\n", tokens.Colors.Accent, row.label, escape(text(component, row.field)))
		}
		out.WriteString("</section>\n")
	case "image-text":
		openCard(out, component.Name, tokens)
		writeImage(out, text(component, "image"), text(component, "alt"), tokens)
		componentTitle(out, text(component, "title"), tokens)
		paragraph(out, text(component, "content"), "margin:8px 0 0;", tokens)
		out.WriteString("</section>\n")
	case "gallery":
		openCard(out, component.Name, tokens)
		componentTitle(out, text(component, "title"), tokens)
		for _, item := range objectsFor(component, "images") {
			out.WriteString("<figure style=\"margin:16px 0;\">")
			writeImage(out, stringValue(item, "src"), stringValue(item, "alt"), tokens)
			caption(out, stringValue(item, "caption"), tokens)
			out.WriteString("</figure>\n")
		}
		out.WriteString("</section>\n")
	case "code-card":
		open(out, "section", component.Name, fmt.Sprintf("background:%s;border-radius:%dpx;margin:24px 0;overflow:hidden;", tokens.Colors.CodeBackground, tokens.Shape.Radius))
		fmt.Fprintf(out, "<p style=\"border-bottom:1px solid #334155;color:%s;font-size:13px;font-weight:700;margin:0;padding:10px 14px;\">%s · %s</p>\n", tokens.Colors.CodeText, escape(text(component, "title")), escape(text(component, "language")))
		fmt.Fprintf(out, "<pre style=\"background:%s;color:%s;font-family:%s;font-size:%dpx;line-height:%.2f;margin:0;overflow-x:auto;padding:16px;white-space:pre-wrap;\"><code>%s</code></pre>\n", tokens.Colors.CodeBackground, tokens.Colors.CodeText, escape(tokens.Code.FontFamily), tokens.Code.FontSize, tokens.Code.LineHeight, escape(text(component, "code")))
		out.WriteString("</section>\n")
	case "data-card":
		openCard(out, component.Name, tokens)
		componentTitle(out, text(component, "title"), tokens)
		out.WriteString("<table style=\"border-collapse:collapse;width:100%;\">\n")
		for _, row := range objectsFor(component, "rows") {
			fmt.Fprintf(out, "<tr><th style=\"border-bottom:1px solid %s;color:%s;padding:9px;text-align:left;\">%s</th><td style=\"border-bottom:1px solid %s;padding:9px;text-align:right;\">%s</td></tr>\n", tokens.Colors.Border, tokens.Colors.Muted, escape(stringValue(row, "label")), tokens.Colors.Border, escape(stringValue(row, "value")))
		}
		out.WriteString("</table></section>\n")
	case "summary":
		open(out, "section", component.Name, fmt.Sprintf("background:%s;border-top:4px solid %s;margin:32px 0;padding:20px;", tokens.Colors.AccentSoft, tokens.Colors.Accent))
		componentTitle(out, text(component, "title"), tokens)
		paragraph(out, text(component, "text"), "margin:8px 0 0;", tokens)
		out.WriteString("</section>\n")
	case "author":
		openCard(out, component.Name, tokens)
		if avatar := text(component, "avatar"); avatar != "" {
			fmt.Fprintf(out, "<img src=\"%s\" alt=\"%s\" style=\"border-radius:50%%;height:56px;object-fit:cover;width:56px;\">\n", escape(safeURL(avatar)), escape(text(component, "name")))
		}
		componentTitle(out, text(component, "name"), tokens)
		paragraph(out, text(component, "bio"), "color:"+tokens.Colors.Muted+";margin:6px 0 0;", tokens)
		out.WriteString("</section>\n")
	case "cta":
		open(out, "section", component.Name, fmt.Sprintf("background:%s;border-radius:%dpx;margin:28px 0;padding:24px;text-align:center;", tokens.Colors.AccentSoft, tokens.Shape.Radius))
		componentTitle(out, text(component, "title"), tokens)
		paragraph(out, text(component, "text"), "margin:8px 0 16px;", tokens)
		fmt.Fprintf(out, "<a href=\"%s\" style=\"background:%s;border-radius:%dpx;color:#FFFFFF;display:inline-block;font-weight:700;padding:10px 20px;text-decoration:none;\">%s</a>\n", escape(safeURL(text(component, "url"))), tokens.Colors.Accent, tokens.Shape.Radius, escape(text(component, "button_label")))
		out.WriteString("</section>\n")
	case "subscribe":
		open(out, "section", component.Name, fmt.Sprintf("border:1px dashed %s;margin:28px 0;padding:20px;text-align:center;", tokens.Colors.Accent))
		componentTitle(out, text(component, "title"), tokens)
		paragraph(out, text(component, "text"), "color:"+tokens.Colors.Muted+";margin:6px 0 0;", tokens)
		out.WriteString("</section>\n")
	}
}

func writeCallout(out *bytes.Buffer, component componentBlock, tokens catalog.ThemeTokens) {
	background, border := tokens.Colors.AccentSoft, tokens.Colors.Accent
	switch text(component, "tone") {
	case "warning":
		background, border = "#FFFBEB", "#D97706"
	case "success":
		background, border = "#ECFDF5", "#059669"
	case "neutral":
		background, border = tokens.Colors.Surface, tokens.Colors.Muted
	}
	open(out, "aside", component.Name, fmt.Sprintf("background:%s;border-left:5px solid %s;border-radius:%dpx;margin:24px 0;padding:16px 18px;", background, border, tokens.Shape.Radius))
	componentTitle(out, text(component, "title"), tokens)
	paragraph(out, text(component, "content"), "margin:6px 0 0;", tokens)
	out.WriteString("</aside>\n")
}

func writeComparison(out *bytes.Buffer, component componentBlock, tokens catalog.ThemeTokens) {
	openCard(out, component.Name, tokens)
	if title := text(component, "title"); title != "" {
		componentTitle(out, title, tokens)
	}
	leftTitle, rightTitle := text(component, "left_title"), text(component, "right_title")
	leftItems, rightItems := stringsFor(component, "left_items"), stringsFor(component, "right_items")
	if component.Name == "pros-cons" {
		leftTitle, rightTitle, leftItems, rightItems = "优势", "权衡", stringsFor(component, "pros"), stringsFor(component, "cons")
	}
	fmt.Fprintf(out, "<table role=\"presentation\" style=\"border-collapse:collapse;width:100%%;\"><tr><th style=\"background:%s;border:1px solid %s;color:%s;padding:10px;\">%s</th><th style=\"background:%s;border:1px solid %s;color:%s;padding:10px;\">%s</th></tr><tr><td style=\"border:1px solid %s;padding:10px;vertical-align:top;\">", tokens.Colors.AccentSoft, tokens.Colors.Border, tokens.Colors.Heading, escape(leftTitle), tokens.Colors.Surface, tokens.Colors.Border, tokens.Colors.Heading, escape(rightTitle), tokens.Colors.Border)
	writeList(out, leftItems, false, false, tokens)
	fmt.Fprintf(out, "</td><td style=\"border:1px solid %s;padding:10px;vertical-align:top;\">", tokens.Colors.Border)
	writeList(out, rightItems, false, false, tokens)
	out.WriteString("</td></tr></table></section>\n")
}

func open(out *bytes.Buffer, tag, name, style string) {
	fmt.Fprintf(out, "<%s data-wx-component=\"%s\" style=\"%s\">\n", tag, escape(name), style)
}
func openCard(out *bytes.Buffer, name string, tokens catalog.ThemeTokens) {
	open(out, "section", name, fmt.Sprintf("background:%s;border:1px solid %s;border-radius:%dpx;box-shadow:%s;margin:24px 0;padding:20px;", tokens.Colors.Surface, tokens.Colors.Border, tokens.Shape.Radius, tokens.Shape.Shadow))
}
func label(out *bytes.Buffer, value string, tokens catalog.ThemeTokens) {
	if value != "" {
		fmt.Fprintf(out, "<p style=\"color:%s;font-size:12px;font-weight:700;letter-spacing:.12em;margin:0 0 8px;text-transform:uppercase;\">%s</p>\n", tokens.Colors.Accent, escape(value))
	}
}
func heading(out *bytes.Buffer, tag, value string, size int, tokens catalog.ThemeTokens) {
	if value != "" {
		fmt.Fprintf(out, "<%s style=\"color:%s;font-family:%s;font-size:%dpx;line-height:1.35;margin:0;max-width:100%%;overflow-wrap:anywhere;word-break:keep-all;\">%s</%s>\n", tag, tokens.Colors.Heading, escape(tokens.Typography.HeadingFontFamily), size, escape(value), tag)
	}
}
func componentTitle(out *bytes.Buffer, value string, tokens catalog.ThemeTokens) {
	heading(out, "h3", value, tokens.Typography.H3Size, tokens)
}
func paragraph(out *bytes.Buffer, value, style string, tokens catalog.ThemeTokens) {
	if value != "" {
		fmt.Fprintf(out, "<p style=\"color:%s;%s\">%s</p>\n", tokens.Colors.Text, style, escape(value))
	}
}
func caption(out *bytes.Buffer, value string, tokens catalog.ThemeTokens) {
	if value != "" {
		fmt.Fprintf(out, "<figcaption style=\"color:%s;font-size:%dpx;line-height:%.2f;margin-top:6px;text-align:center;\">%s</figcaption>", tokens.Caption.Color, tokens.Caption.FontSize, tokens.Caption.LineHeight, escape(value))
	}
}
func writeImage(out *bytes.Buffer, src, alt string, tokens catalog.ThemeTokens) {
	fmt.Fprintf(out, "<img src=\"%s\" alt=\"%s\" style=\"border-radius:%dpx;display:block;height:auto;max-width:100%%;width:100%%;\">\n", escape(safeURL(src)), escape(alt), tokens.Shape.ImageRadius)
}

func writeList(out *bytes.Buffer, items []string, checklist, ordered bool, tokens catalog.ThemeTokens) {
	tag := "ul"
	if ordered {
		tag = "ol"
	}
	fmt.Fprintf(out, "<%s style=\"margin:10px 0;padding-left:22px;\">\n", tag)
	for _, item := range items {
		prefix := ""
		if checklist {
			prefix = "✓ "
		}
		fmt.Fprintf(out, "<li style=\"margin:6px 0;\"><span style=\"color:%s;\">%s%s</span></li>\n", tokens.Colors.Text, prefix, escape(item))
	}
	fmt.Fprintf(out, "</%s>\n", tag)
}

func text(component componentBlock, key string) string { return stringValue(component.Fields, key) }
func stringValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}
func stringsFor(component componentBlock, key string) []string {
	values, _ := component.Fields[key].([]any)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if item, ok := value.(string); ok {
			result = append(result, item)
		}
	}
	return result
}
func objectsFor(component componentBlock, key string) []map[string]any {
	values, _ := component.Fields[key].([]any)
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if item, ok := value.(map[string]any); ok {
			result = append(result, item)
		}
	}
	return result
}
func escape(value string) string { return html.EscapeString(value) }

func safeURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	if parsed.Scheme == "" && (strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") || strings.HasPrefix(value, "/")) {
		return value
	}
	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		return value
	}
	return ""
}

func inlineStyles(rendered string, tokens catalog.ThemeTokens) string {
	replacer := strings.NewReplacer(
		"<h1>", fmt.Sprintf(`<h1 style="color:%s;font-family:%s;font-size:%dpx;line-height:1.35;margin:1.4em 0 .7em;max-width:100%%;overflow-wrap:anywhere;word-break:keep-all;">`, tokens.Colors.Heading, escape(tokens.Typography.HeadingFontFamily), tokens.Typography.H1Size),
		"<h2>", fmt.Sprintf(`<h2 style="color:%s;font-family:%s;font-size:%dpx;line-height:1.4;margin:1.5em 0 .7em;max-width:100%%;overflow-wrap:anywhere;word-break:keep-all;">`, tokens.Colors.Heading, escape(tokens.Typography.HeadingFontFamily), tokens.Typography.H2Size),
		"<h3>", fmt.Sprintf(`<h3 style="color:%s;font-family:%s;font-size:%dpx;line-height:1.45;margin:1.4em 0 .6em;max-width:100%%;overflow-wrap:anywhere;word-break:keep-all;">`, tokens.Colors.Heading, escape(tokens.Typography.HeadingFontFamily), tokens.Typography.H3Size),
		"<p>", fmt.Sprintf(`<p style="color:%s;margin:1em 0;">`, tokens.Colors.Text),
		"<a ", fmt.Sprintf(`<a style="color:%s;text-decoration:underline;" `, tokens.Colors.Accent),
		"<blockquote>", fmt.Sprintf(`<blockquote style="border-left:4px solid %s;color:%s;margin:1.2em 0;padding-left:16px;">`, tokens.Colors.Accent, tokens.Colors.Muted),
		"<pre>", fmt.Sprintf(`<pre style="background:%s;border-radius:%dpx;color:%s;font-family:%s;overflow-x:auto;padding:16px;white-space:pre-wrap;">`, tokens.Colors.CodeBackground, tokens.Shape.Radius, tokens.Colors.CodeText, escape(tokens.Code.FontFamily)),
		"<table>", `<table style="border-collapse:collapse;margin:1.2em 0;width:100%;">`,
		"<th>", fmt.Sprintf(`<th style="background:%s;border:1px solid %s;font-weight:600;padding:8px 12px;text-align:left;">`, tokens.Colors.Surface, tokens.Colors.Border),
		"<td>", fmt.Sprintf(`<td style="border:1px solid %s;padding:8px 12px;">`, tokens.Colors.Border),
		"<img ", fmt.Sprintf(`<img style="border-radius:%dpx;height:auto;max-width:100%%;" `, tokens.Shape.ImageRadius),
	)
	return strings.ReplaceAll(replacer.Replace(rendered), "<!-- raw HTML omitted -->", "")
}
