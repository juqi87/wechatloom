package builder

import (
	"context"
	"fmt"
	"strings"

	"github.com/wechatloom/wechatloom/internal/catalog"
	"go.yaml.in/yaml/v3"
)

type componentBlock struct {
	Name   string
	Line   int
	Fields map[string]any
}

type documentSegment struct {
	Markdown  []byte
	Component *componentBlock
}

func inspectComponents(ctx context.Context, body []byte) (int, int, []Diagnostic) {
	blocks, diagnostics := scanComponentBlocks(body)
	calloutCount := 0
	builtin := catalog.NewBuiltin()
	for _, block := range blocks {
		if block.Name == "callout" {
			calloutCount++
		}
		validation, err := builtin.Validate(ctx, catalog.ValidationRequest{
			Ref:    catalog.Ref{Kind: catalog.KindComponent, Name: block.Name},
			Fields: block.Fields,
		})
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{
				Code: "UNKNOWN_COMPONENT", Message: err.Error(), Line: block.Line,
			})
			continue
		}
		if validation.Valid {
			continue
		}
		messages := make([]string, 0, len(validation.Violations))
		for _, violation := range validation.Violations {
			messages = append(messages, violation.Message)
		}
		diagnostics = append(diagnostics, Diagnostic{
			Code:    "COMPONENT_SCHEMA_INVALID",
			Message: fmt.Sprintf("wx-%s: %s", block.Name, strings.Join(messages, "; ")),
			Line:    block.Line,
		})
	}
	return len(blocks), calloutCount, diagnostics
}

func scanComponentBlocks(body []byte) ([]componentBlock, []Diagnostic) {
	lines := strings.Split(string(body), "\n")
	blocks := make([]componentBlock, 0)
	diagnostics := make([]Diagnostic, 0)
	for index := 0; index < len(lines); index++ {
		name, opening := componentOpening(lines[index])
		if !opening {
			continue
		}
		startLine := index + 1
		fieldsStart := index + 1
		closing := fieldsStart
		nested := false
		for ; closing < len(lines) && strings.TrimSpace(lines[closing]) != ":::"; closing++ {
			if _, isOpening := componentOpening(lines[closing]); isOpening {
				diagnostics = append(diagnostics, Diagnostic{
					Code: "NESTED_COMPONENT", Message: "WeChatLoom components cannot be nested", Line: closing + 1,
				})
				nested = true
			}
		}
		if closing >= len(lines) {
			diagnostics = append(diagnostics, Diagnostic{
				Code: "UNCLOSED_COMPONENT", Message: fmt.Sprintf("wx-%s is missing its closing delimiter", name), Line: startLine,
			})
			break
		}
		if !nested {
			fields := map[string]any{}
			encoded := strings.Join(lines[fieldsStart:closing], "\n")
			if err := yaml.Unmarshal([]byte(encoded), &fields); err != nil {
				diagnostics = append(diagnostics, Diagnostic{
					Code: "COMPONENT_SCHEMA_INVALID", Message: fmt.Sprintf("decode wx-%s: %v", name, err), Line: startLine,
				})
			} else {
				blocks = append(blocks, componentBlock{Name: name, Line: startLine, Fields: fields})
			}
		}
		index = closing
	}
	return blocks, diagnostics
}

func componentOpening(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, ":::wx-") {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimPrefix(trimmed, ":::wx-"))
	if name == "" || strings.ContainsAny(name, " \t") {
		return "", false
	}
	return name, true
}

func splitComponents(body []byte) ([]documentSegment, []string, error) {
	lines := strings.Split(string(body), "\n")
	segments := make([]documentSegment, 0)
	components := make([]string, 0)
	markdownLines := make([]string, 0)
	flushMarkdown := func() {
		if len(markdownLines) == 0 {
			return
		}
		segments = append(segments, documentSegment{Markdown: []byte(strings.Join(markdownLines, "\n") + "\n")})
		markdownLines = nil
	}

	for index := 0; index < len(lines); index++ {
		name, opening := componentOpening(lines[index])
		if !opening {
			markdownLines = append(markdownLines, lines[index])
			continue
		}
		flushMarkdown()
		startLine := index + 1
		fieldsStart := index + 1
		closing := fieldsStart
		for ; closing < len(lines) && strings.TrimSpace(lines[closing]) != ":::"; closing++ {
			if _, nested := componentOpening(lines[closing]); nested {
				return nil, nil, fmt.Errorf("line %d: WeChatLoom components cannot be nested", closing+1)
			}
		}
		if closing >= len(lines) {
			return nil, nil, fmt.Errorf("line %d: wx-%s is missing its closing delimiter", startLine, name)
		}
		fields := map[string]any{}
		if err := yaml.Unmarshal([]byte(strings.Join(lines[fieldsStart:closing], "\n")), &fields); err != nil {
			return nil, nil, fmt.Errorf("line %d: decode wx-%s: %w", startLine, name, err)
		}
		block := componentBlock{Name: name, Line: startLine, Fields: fields}
		segments = append(segments, documentSegment{Component: &block})
		components = append(components, name)
		index = closing
	}
	flushMarkdown()
	return segments, components, nil
}
