package catalog

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

type ComponentSchema struct {
	Schema               string                    `json:"$schema"`
	ID                   string                    `json:"$id"`
	Title                string                    `json:"title"`
	Type                 string                    `json:"type"`
	AdditionalProperties bool                      `json:"additionalProperties"`
	Required             []string                  `json:"required,omitempty"`
	Properties           map[string]SchemaProperty `json:"properties"`
}

type SchemaProperty struct {
	Type                 string                    `json:"type"`
	Description          string                    `json:"description,omitempty"`
	MinLength            int                       `json:"minLength,omitempty"`
	MinItems             int                       `json:"minItems,omitempty"`
	Enum                 []string                  `json:"enum,omitempty"`
	Items                *SchemaProperty           `json:"items,omitempty"`
	AdditionalProperties *bool                     `json:"additionalProperties,omitempty"`
	Required             []string                  `json:"required,omitempty"`
	Properties           map[string]SchemaProperty `json:"properties,omitempty"`
}

func ComponentJSONSchema(definition ComponentDefinition) ComponentSchema {
	schema := ComponentSchema{
		Schema: "https://json-schema.org/draft/2020-12/schema",
		ID:     "https://wechatloom.dev/schemas/components/wx-" + definition.Name + ".schema.json",
		Title:  "WeChatLoom " + definition.Name + " component",
		Type:   "object", AdditionalProperties: false,
		Properties: map[string]SchemaProperty{},
	}
	for _, field := range definition.Fields {
		if field.Required {
			schema.Required = append(schema.Required, field.Name)
		}
		schema.Properties[field.Name] = schemaProperty(field)
	}
	return schema
}

func schemaProperty(field FieldDefinition) SchemaProperty {
	property := SchemaProperty{Description: field.Description, Enum: field.Enum}
	switch field.Type {
	case "string":
		property.Type = "string"
		property.MinLength = 1
	case "string_list":
		property.Type = "array"
		property.MinItems = 1
		property.Items = &SchemaProperty{Type: "string", MinLength: 1}
	case "object_list":
		property.Type = "array"
		property.MinItems = 1
		additional := false
		item := SchemaProperty{Type: "object", AdditionalProperties: &additional, Properties: map[string]SchemaProperty{}}
		for _, nested := range field.Fields {
			if nested.Required {
				item.Required = append(item.Required, nested.Name)
			}
			item.Properties[nested.Name] = schemaProperty(nested)
		}
		property.Items = &item
	}
	return property
}

func (builtin *Builtin) Validate(
	ctx context.Context,
	request ValidationRequest,
) (ValidationResult, error) {
	if err := ctx.Err(); err != nil {
		return ValidationResult{}, err
	}
	if request.Ref.Kind != KindComponent {
		return ValidationResult{}, fmt.Errorf("validation is unsupported for resource kind %q", request.Ref.Kind)
	}
	definition, err := builtin.Show(ctx, request.Ref)
	if err != nil {
		return ValidationResult{}, err
	}

	violations := validateFields(request.Fields, definition.Component.Fields, "")
	if request.Ref.Name == "cta" {
		if rawURL, ok := request.Fields["url"].(string); ok && strings.TrimSpace(rawURL) != "" {
			parsed, err := url.Parse(rawURL)
			if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
				violations = append(violations, Violation{Field: "url", Code: "URL", Message: "url must be an absolute HTTPS URL"})
			}
		}
	}
	return ValidationResult{Valid: len(violations) == 0, Violations: violations}, nil
}

func validateFields(values map[string]any, fields []FieldDefinition, prefix string) []Violation {
	violations := make([]Violation, 0)
	known := make(map[string]bool, len(fields))
	for _, field := range fields {
		known[field.Name] = true
		path := field.Name
		if prefix != "" {
			path = prefix + "." + field.Name
		}
		value, present := values[field.Name]
		if !present || emptyValue(value) {
			if field.Required {
				violations = append(violations, Violation{
					Field: path, Code: "REQUIRED", Message: fmt.Sprintf("%s is required", path),
				})
			}
			continue
		}

		switch field.Type {
		case "string":
			text, ok := value.(string)
			if !ok {
				violations = append(violations, typeViolation(path, "string"))
				continue
			}
			if len(field.Enum) != 0 && !contains(field.Enum, text) {
				violations = append(violations, Violation{
					Field: path, Code: "ENUM",
					Message: fmt.Sprintf("%s must be one of %s", path, strings.Join(field.Enum, ", ")),
				})
			}
		case "string_list":
			items, ok := asSlice(value)
			if !ok {
				violations = append(violations, typeViolation(path, "list of strings"))
				continue
			}
			for index, item := range items {
				text, ok := item.(string)
				if !ok || strings.TrimSpace(text) == "" {
					violations = append(violations, typeViolation(fmt.Sprintf("%s[%d]", path, index), "non-empty string"))
				}
			}
		case "object_list":
			items, ok := asSlice(value)
			if !ok {
				violations = append(violations, typeViolation(path, "list of objects"))
				continue
			}
			for index, item := range items {
				object, ok := item.(map[string]any)
				if !ok {
					violations = append(violations, typeViolation(fmt.Sprintf("%s[%d]", path, index), "object"))
					continue
				}
				violations = append(violations, validateFields(object, field.Fields, fmt.Sprintf("%s[%d]", path, index))...)
			}
		}
	}
	unknownNames := make([]string, 0)
	for name := range values {
		if !known[name] {
			unknownNames = append(unknownNames, name)
		}
	}
	sort.Strings(unknownNames)
	for _, name := range unknownNames {
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		violations = append(violations, Violation{Field: path, Code: "UNKNOWN_FIELD", Message: fmt.Sprintf("%s is not defined by the component schema", path)})
	}
	return violations
}

func emptyValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case []string:
		return len(typed) == 0
	default:
		return false
	}
}

func asSlice(value any) ([]any, bool) {
	switch typed := value.(type) {
	case []any:
		return typed, true
	case []string:
		items := make([]any, len(typed))
		for index, item := range typed {
			items[index] = item
		}
		return items, true
	default:
		return nil, false
	}
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func typeViolation(field, want string) Violation {
	return Violation{Field: field, Code: "TYPE", Message: fmt.Sprintf("%s must be a %s", field, want)}
}

func componentDefinition(resource ResourceSummary) ComponentDefinition {
	fields := componentFields(resource.Name)
	return ComponentDefinition{
		Name:           resource.Name,
		Category:       resource.Family,
		Version:        resource.Version,
		Description:    resource.Description,
		SchemaVersion:  "1",
		Fields:         fields,
		ValidExample:   componentExample(resource.Name, fields, false),
		InvalidExample: componentExample(resource.Name, fields, true),
	}
}

func componentFields(name string) []FieldDefinition {
	switch name {
	case "hero":
		return []FieldDefinition{
			stringField("title", true, "Primary article title"),
			stringField("subtitle", true, "Supporting subtitle"),
			stringField("eyebrow", false, "Short category label"),
		}
	case "lead":
		return []FieldDefinition{stringField("text", true, "Opening paragraph text")}
	case "toc":
		return []FieldDefinition{
			stringField("title", false, "Table-of-contents heading"),
			stringListField("items", true, "Section titles in reading order"),
		}
	case "audience":
		return []FieldDefinition{
			stringField("title", true, "Audience block heading"),
			stringListField("items", true, "Intended reader descriptions"),
		}
	case "section":
		return []FieldDefinition{
			stringField("title", true, "Section title"),
			stringField("kicker", false, "Short section label"),
		}
	case "divider":
		return []FieldDefinition{stringField("label", true, "Optional-looking but accessible divider label")}
	case "steps":
		return []FieldDefinition{
			stringField("title", false, "Steps block heading"),
			stringListField("items", true, "Ordered step descriptions"),
		}
	case "timeline":
		return []FieldDefinition{
			stringField("title", false, "Timeline heading"),
			objectListField("items", true, "Chronological entries", []FieldDefinition{
				stringField("time", true, "Date or phase label"),
				stringField("title", true, "Event title"),
				stringField("text", false, "Event detail"),
			}),
		}
	case "checklist":
		return []FieldDefinition{
			stringField("title", false, "Checklist heading"),
			stringListField("items", true, "Checklist entries"),
		}
	case "callout":
		return []FieldDefinition{
			stringField("title", true, "Callout heading"),
			enumField("tone", false, "Semantic callout tone", "info", "warning", "success", "neutral"),
			stringField("content", true, "Callout body"),
		}
	case "quote":
		return []FieldDefinition{
			stringField("text", true, "Quotation text"),
			stringField("attribution", true, "Person or source"),
		}
	case "metrics":
		return []FieldDefinition{
			stringField("title", false, "Metrics heading"),
			objectListField("items", true, "Metric tiles", []FieldDefinition{
				stringField("value", true, "Metric value"),
				stringField("label", true, "Metric label"),
			}),
		}
	case "compare":
		return []FieldDefinition{
			stringField("left_title", true, "Left-column title"),
			stringListField("left_items", true, "Left-column points"),
			stringField("right_title", true, "Right-column title"),
			stringListField("right_items", true, "Right-column points"),
		}
	case "case":
		return []FieldDefinition{
			stringField("title", true, "Case-study title"),
			stringField("challenge", true, "Initial challenge"),
			stringField("solution", true, "Applied solution"),
			stringField("result", true, "Observed result"),
		}
	case "pros-cons":
		return []FieldDefinition{
			stringField("title", false, "Block heading"),
			stringListField("pros", true, "Benefits"),
			stringListField("cons", true, "Tradeoffs"),
		}
	case "image-text":
		return []FieldDefinition{
			stringField("image", true, "Local or resolved image path"),
			stringField("alt", true, "Accessible image description"),
			stringField("title", true, "Text heading"),
			stringField("content", true, "Explanatory text"),
			enumField("position", false, "Image placement", "top", "left", "right"),
		}
	case "gallery":
		return []FieldDefinition{
			stringField("title", false, "Gallery heading"),
			objectListField("images", true, "Captioned images", []FieldDefinition{
				stringField("src", true, "Local or resolved image path"),
				stringField("alt", true, "Accessible image description"),
				stringField("caption", false, "Visible image caption"),
			}),
		}
	case "code-card":
		return []FieldDefinition{
			stringField("title", true, "Code example title"),
			stringField("language", true, "Programming language label"),
			stringField("code", true, "Source code"),
		}
	case "data-card":
		return []FieldDefinition{
			stringField("title", true, "Data-card title"),
			objectListField("rows", true, "Label and value rows", []FieldDefinition{
				stringField("label", true, "Row label"),
				stringField("value", true, "Row value"),
			}),
		}
	case "summary":
		return []FieldDefinition{
			stringField("title", false, "Summary heading"),
			stringField("text", true, "Summary text"),
		}
	case "takeaways":
		return []FieldDefinition{
			stringField("title", false, "Takeaways heading"),
			stringListField("items", true, "Key takeaways"),
		}
	case "author":
		return []FieldDefinition{
			stringField("name", true, "Author name"),
			stringField("bio", true, "Short author biography"),
			stringField("avatar", false, "Local or resolved avatar path"),
		}
	case "cta":
		return []FieldDefinition{
			stringField("title", true, "Action heading"),
			stringField("text", true, "Action explanation"),
			stringField("button_label", true, "Action-link label"),
			stringField("url", true, "HTTPS action URL"),
		}
	case "subscribe":
		return []FieldDefinition{
			stringField("title", true, "Subscription heading"),
			stringField("text", true, "Subscription explanation"),
		}
	default:
		return []FieldDefinition{}
	}
}

func stringField(name string, required bool, description string) FieldDefinition {
	return FieldDefinition{Name: name, Type: "string", Required: required, Description: description}
}

func enumField(name string, required bool, description string, values ...string) FieldDefinition {
	return FieldDefinition{
		Name: name, Type: "string", Required: required, Description: description, Enum: values,
	}
}

func stringListField(name string, required bool, description string) FieldDefinition {
	return FieldDefinition{Name: name, Type: "string_list", Required: required, Description: description}
}

func objectListField(
	name string,
	required bool,
	description string,
	fields []FieldDefinition,
) FieldDefinition {
	return FieldDefinition{
		Name: name, Type: "object_list", Required: required, Description: description, Fields: fields,
	}
}

func componentExample(name string, fields []FieldDefinition, invalid bool) string {
	values := componentSampleValues(name)
	if invalid {
		for _, field := range fields {
			if field.Required {
				delete(values, field.Name)
				break
			}
		}
	}
	encoded, err := yaml.Marshal(values)
	if err != nil {
		encoded = []byte("invalid: true\n")
	}
	var output strings.Builder
	output.WriteString(":::wx-")
	output.WriteString(name)
	output.WriteByte('\n')
	output.Write(encoded)
	output.WriteString(":::\n")
	return output.String()
}

func componentSampleValues(name string) map[string]any {
	values := map[string]map[string]any{
		"hero":       {"title": "把 Markdown 稳定送进公众号", "subtitle": "先构建，再预览", "eyebrow": "WeChatLoom"},
		"lead":       {"text": "这是一段强调文章价值的开场文字。"},
		"toc":        {"title": "本文目录", "items": []string{"准备内容", "检查排版", "确认草稿"}},
		"audience":   {"title": "适合谁阅读", "items": []string{"公众号作者", "内容运营者"}},
		"section":    {"title": "开始构建", "kicker": "第一部分"},
		"divider":    {"label": "继续阅读"},
		"steps":      {"title": "操作步骤", "items": []string{"检查 Markdown", "生成预览", "确认结果"}},
		"timeline":   {"title": "发布流程", "items": []map[string]any{{"time": "第 1 步", "title": "本地构建", "text": "生成稳定 HTML"}, {"time": "第 2 步", "title": "人工确认", "text": "检查手机预览"}}},
		"checklist":  {"title": "发布前检查", "items": []string{"标题完整", "图片清晰", "链接安全"}},
		"callout":    {"title": "注意", "tone": "info", "content": "本地构建不会创建微信草稿。"},
		"quote":      {"text": "稳定的流程比临时修补更可靠。", "attribution": "WeChatLoom"},
		"metrics":    {"title": "核心指标", "items": []map[string]any{{"value": "24", "label": "原创主题"}, {"value": "24", "label": "布局组件"}}},
		"compare":    {"left_title": "手工排版", "left_items": []string{"容易漂移", "难以复现"}, "right_title": "WeChatLoom", "right_items": []string{"构建稳定", "记录清晰"}},
		"case":       {"title": "一篇技术长文", "challenge": "格式复杂", "solution": "使用主题与组件", "result": "输出可复现"},
		"pros-cons":  {"title": "方案权衡", "pros": []string{"本地优先", "原稿只读"}, "cons": []string{"仍需人工预览"}},
		"image-text": {"image": "./images/cover.png", "alt": "文章封面示意图", "title": "图文说明", "content": "图片与文字共同解释核心观点。", "position": "top"},
		"gallery":    {"title": "旅行图集", "images": []map[string]any{{"src": "./images/a.png", "alt": "清晨的山", "caption": "日出时分"}, {"src": "./images/b.png", "alt": "安静的湖", "caption": "湖畔傍晚"}}},
		"code-card":  {"title": "构建命令", "language": "bash", "code": "wechatloom build article.md --json"},
		"data-card":  {"title": "构建事实", "rows": []map[string]any{{"label": "主题", "value": "minimal"}, {"label": "状态", "value": "completed"}}},
		"summary":    {"title": "总结", "text": "先建立稳定的本地结果，再连接远程草稿。"},
		"takeaways":  {"title": "关键收获", "items": []string{"原稿保持不变", "输出可以复现", "远程写入需要确认"}},
		"author":     {"name": "WeChatLoom", "bio": "专注于稳定的公众号内容工作流。", "avatar": "./images/avatar.png"},
		"cta":        {"title": "查看完整指南", "text": "继续阅读项目文档。", "button_label": "阅读文档", "url": "https://example.com/wechatloom"},
		"subscribe":  {"title": "持续关注", "text": "订阅后获取后续版本更新。"},
	}
	selected := values[name]
	copyOfValues := make(map[string]any, len(selected))
	for key, value := range selected {
		copyOfValues[key] = value
	}
	return copyOfValues
}
