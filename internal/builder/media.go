package builder

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type richMediaResult struct {
	Markdown    []byte
	Assets      map[string][]byte
	Diagnostics []Diagnostic
}

type diagramNode struct {
	ID    string
	Label string
}

type diagramEdge struct {
	From string
	To   string
}

type diagram struct {
	Direction string
	Nodes     []diagramNode
	Edges     []diagramEdge
}

type formulaRun struct {
	Text   string
	Script int
}

var unsafeMermaid = regexp.MustCompile(`(?i)(<\s*script|javascript\s*:|\bclick\s+|https?://|\bstyle\s+)`)

func processRichMedia(body []byte) richMediaResult {
	lines := strings.Split(string(body), "\n")
	output := make([]string, 0, len(lines))
	assets := map[string][]byte{}
	diagnostics := make([]Diagnostic, 0)
	for index := 0; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "$$" {
			startLine := index + 1
			closing := index + 1
			for ; closing < len(lines) && strings.TrimSpace(lines[closing]) != "$$"; closing++ {
			}
			if closing >= len(lines) {
				diagnostics = append(diagnostics, Diagnostic{Code: "FORMULA_UNCLOSED", Message: "display formula is missing its closing $$", Line: startLine})
				output = append(output, lines[index:]...)
				break
			}
			expression := strings.TrimSpace(strings.Join(lines[index+1:closing], " "))
			if expression == "" {
				diagnostics = append(diagnostics, Diagnostic{Code: "FORMULA_EMPTY", Message: "display formula cannot be empty", Line: startLine})
				output = append(output, lines[index:closing+1]...)
				index = closing
				continue
			}
			assetPath := generatedAssetPath("formula", expression)
			encoded, err := renderFormulaPNG(expression)
			if err != nil {
				diagnostics = append(diagnostics, Diagnostic{Code: "FORMULA_RENDER_FAILED", Message: err.Error(), Line: startLine})
				output = append(output, lines[index:closing+1]...)
			} else {
				assets[assetPath] = encoded
				output = append(output, fmt.Sprintf("![%s](%s)", markdownAlt("公式："+expression), assetPath))
			}
			index = closing
			continue
		}
		if strings.EqualFold(trimmed, "```mermaid") {
			startLine := index + 1
			closing := index + 1
			for ; closing < len(lines) && strings.TrimSpace(lines[closing]) != "```"; closing++ {
			}
			if closing >= len(lines) {
				diagnostics = append(diagnostics, Diagnostic{Code: "MERMAID_UNCLOSED", Message: "Mermaid block is missing its closing fence", Line: startLine})
				output = append(output, lines[index:]...)
				break
			}
			source := strings.TrimSpace(strings.Join(lines[index+1:closing], "\n"))
			if unsafeMermaid.MatchString(source) {
				diagnostics = append(diagnostics, Diagnostic{Code: "MERMAID_UNSAFE", Message: "Mermaid block contains links, click handlers, styles, or executable markup", Line: startLine})
				output = append(output, lines[index:closing+1]...)
				index = closing
				continue
			}
			parsed, err := parseMermaid(source)
			if err != nil {
				diagnostics = append(diagnostics, Diagnostic{Code: "MERMAID_PARSE_ERROR", Message: err.Error(), Line: startLine})
				output = append(output, lines[index:closing+1]...)
				index = closing
				continue
			}
			encoded, err := renderMermaidPNG(parsed)
			if err != nil {
				diagnostics = append(diagnostics, Diagnostic{Code: "MERMAID_RENDER_FAILED", Message: err.Error(), Line: startLine})
				output = append(output, lines[index:closing+1]...)
			} else {
				assetPath := generatedAssetPath("mermaid", source)
				assets[assetPath] = encoded
				output = append(output, fmt.Sprintf("![%s](%s)", markdownAlt(diagramAlt(parsed)), assetPath))
			}
			index = closing
			continue
		}
		output = append(output, lines[index])
	}
	return richMediaResult{Markdown: []byte(strings.Join(output, "\n")), Assets: assets, Diagnostics: diagnostics}
}

func generatedAssetPath(kind, source string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + source))
	return fmt.Sprintf("assets/%s-%x.png", kind, sum[:8])
}

func markdownAlt(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "]", "）")
	return strings.TrimSpace(value)
}

func parseMermaid(source string) (diagram, error) {
	lines := strings.Split(source, "\n")
	if len(lines) == 0 {
		return diagram{}, fmt.Errorf("Mermaid diagram is empty")
	}
	header := strings.Fields(strings.TrimSpace(lines[0]))
	if len(header) != 2 || (header[0] != "flowchart" && header[0] != "graph") {
		return diagram{}, fmt.Errorf("Mermaid must start with flowchart or graph and a direction")
	}
	direction := strings.ToUpper(header[1])
	if direction != "LR" && direction != "RL" && direction != "TD" && direction != "TB" {
		return diagram{}, fmt.Errorf("unsupported Mermaid direction %q", direction)
	}
	result := diagram{Direction: direction}
	nodeIndexes := map[string]int{}
	addNode := func(node diagramNode) error {
		if node.ID == "" {
			return fmt.Errorf("Mermaid node ID cannot be empty")
		}
		if existing, ok := nodeIndexes[node.ID]; ok {
			if result.Nodes[existing].Label == result.Nodes[existing].ID && node.Label != node.ID {
				result.Nodes[existing].Label = node.Label
			}
			return nil
		}
		nodeIndexes[node.ID] = len(result.Nodes)
		result.Nodes = append(result.Nodes, node)
		return nil
	}
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "%%") {
			continue
		}
		parts := strings.Split(trimmed, "-->")
		if len(parts) != 2 {
			return diagram{}, fmt.Errorf("unsupported Mermaid statement %q; only A --> B edges are allowed", trimmed)
		}
		from, err := parseMermaidNode(parts[0])
		if err != nil {
			return diagram{}, err
		}
		to, err := parseMermaidNode(parts[1])
		if err != nil {
			return diagram{}, err
		}
		if err := addNode(from); err != nil {
			return diagram{}, err
		}
		if err := addNode(to); err != nil {
			return diagram{}, err
		}
		result.Edges = append(result.Edges, diagramEdge{From: from.ID, To: to.ID})
	}
	if len(result.Edges) == 0 {
		return diagram{}, fmt.Errorf("Mermaid diagram must contain at least one edge")
	}
	return result, nil
}

func parseMermaidNode(token string) (diagramNode, error) {
	trimmed := strings.TrimSpace(token)
	identifier := trimmed
	label := ""
	if opening := strings.IndexAny(trimmed, "[({"); opening >= 0 {
		identifier = strings.TrimSpace(trimmed[:opening])
		closing := strings.LastIndexAny(trimmed, "])}")
		if closing <= opening {
			return diagramNode{}, fmt.Errorf("invalid Mermaid node %q", trimmed)
		}
		label = strings.Trim(strings.TrimSpace(trimmed[opening+1:closing]), `"'`)
	}
	for _, character := range identifier {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '_' || character == '-') {
			return diagramNode{}, fmt.Errorf("invalid Mermaid node ID %q", identifier)
		}
	}
	if label == "" {
		label = identifier
	}
	return diagramNode{ID: identifier, Label: label}, nil
}

func renderFormulaPNG(expression string) ([]byte, error) {
	mainFace, err := fontFace(42)
	if err != nil {
		return nil, err
	}
	scriptFace, err := fontFace(27)
	if err != nil {
		return nil, err
	}
	runs := formulaRuns(expression)
	width := 0
	for _, run := range runs {
		face := mainFace
		if run.Script != 0 {
			face = scriptFace
		}
		width += (&font.Drawer{Face: face}).MeasureString(run.Text).Ceil()
	}
	width += 120
	if width < 800 {
		width = 800
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, 220))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.RGBA{248, 250, 252, 255}), image.Point{}, draw.Src)
	totalWidth := width - 120
	x := (width - totalWidth) / 2
	for _, run := range runs {
		face := mainFace
		baseline := 132
		if run.Script > 0 {
			face = scriptFace
			baseline = 96
		}
		if run.Script < 0 {
			face = scriptFace
			baseline = 158
		}
		drawer := &font.Drawer{Dst: canvas, Src: image.NewUniform(color.RGBA{17, 24, 39, 255}), Face: face, Dot: fixedPoint(x, baseline)}
		drawer.DrawString(run.Text)
		x += drawer.MeasureString(run.Text).Ceil()
	}
	return encodePNG(canvas)
}

func formulaRuns(expression string) []formulaRun {
	normalized := expression
	fractions := regexp.MustCompile(`\\frac\{([^{}]+)\}\{([^{}]+)\}`)
	for fractions.MatchString(normalized) {
		normalized = fractions.ReplaceAllString(normalized, "($1)/($2)")
	}
	squares := regexp.MustCompile(`\\sqrt\{([^{}]+)\}`)
	normalized = squares.ReplaceAllString(normalized, "√($1)")
	replacements := map[string]string{
		`\alpha`: "α", `\beta`: "β", `\gamma`: "γ", `\delta`: "δ", `\theta`: "θ",
		`\lambda`: "λ", `\mu`: "μ", `\pi`: "π", `\sigma`: "σ", `\phi`: "φ", `\omega`: "ω",
		`\times`: "×", `\cdot`: "·", `\le`: "≤", `\ge`: "≥", `\neq`: "≠", `\infty`: "∞",
	}
	keys := make([]string, 0, len(replacements))
	for key := range replacements {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, key := range keys {
		normalized = strings.ReplaceAll(normalized, key, replacements[key])
	}
	runes := []rune(normalized)
	runs := make([]formulaRun, 0)
	plain := strings.Builder{}
	flush := func() {
		if plain.Len() != 0 {
			runs = append(runs, formulaRun{Text: plain.String()})
			plain.Reset()
		}
	}
	for index := 0; index < len(runes); index++ {
		if runes[index] != '^' && runes[index] != '_' {
			plain.WriteRune(runes[index])
			continue
		}
		flush()
		script := 1
		if runes[index] == '_' {
			script = -1
		}
		if index+1 >= len(runes) {
			plain.WriteRune(runes[index])
			continue
		}
		index++
		value := strings.Builder{}
		if runes[index] == '{' {
			for index++; index < len(runes) && runes[index] != '}'; index++ {
				value.WriteRune(runes[index])
			}
		} else {
			value.WriteRune(runes[index])
		}
		runs = append(runs, formulaRun{Text: value.String(), Script: script})
	}
	flush()
	return runs
}

func renderMermaidPNG(value diagram) ([]byte, error) {
	face, err := fontFace(30)
	if err != nil {
		return nil, err
	}
	horizontal := value.Direction == "LR" || value.Direction == "RL"
	width, height := 900, 280
	if horizontal {
		width = max(900, len(value.Nodes)*300)
	} else {
		height = max(360, len(value.Nodes)*180)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	positions := make(map[string]image.Point, len(value.Nodes))
	ordered := append([]diagramNode(nil), value.Nodes...)
	if value.Direction == "RL" {
		for left, right := 0, len(ordered)-1; left < right; left, right = left+1, right-1 {
			ordered[left], ordered[right] = ordered[right], ordered[left]
		}
	}
	for index, node := range ordered {
		point := image.Pt(width/2, 100+index*180)
		if horizontal {
			point = image.Pt(150+index*300, height/2)
		}
		positions[node.ID] = point
	}
	lineColor := color.RGBA{14, 116, 144, 255}
	for _, edge := range value.Edges {
		from, to := positions[edge.From], positions[edge.To]
		if horizontal {
			if to.X > from.X {
				from.X += 110
				to.X -= 110
			} else {
				from.X -= 110
				to.X += 110
			}
		} else {
			if to.Y > from.Y {
				from.Y += 52
				to.Y -= 52
			} else {
				from.Y -= 52
				to.Y += 52
			}
		}
		drawLine(canvas, from.X, from.Y, to.X, to.Y, lineColor, 5)
		drawArrow(canvas, from, to, lineColor)
	}
	for _, node := range value.Nodes {
		center := positions[node.ID]
		rect := image.Rect(center.X-110, center.Y-52, center.X+110, center.Y+52)
		draw.Draw(canvas, rect, image.NewUniform(color.RGBA{236, 254, 255, 255}), image.Point{}, draw.Src)
		drawBorder(canvas, rect, lineColor, 5)
		drawer := &font.Drawer{Dst: canvas, Src: image.NewUniform(color.RGBA{17, 24, 39, 255}), Face: face}
		textWidth := drawer.MeasureString(node.Label).Ceil()
		drawer.Dot = fixedPoint(center.X-textWidth/2, center.Y+10)
		drawer.DrawString(node.Label)
	}
	return encodePNG(canvas)
}

func diagramAlt(value diagram) string {
	labels := make([]string, 0, len(value.Nodes))
	for _, node := range value.Nodes {
		labels = append(labels, node.Label)
	}
	return "流程图：" + strings.Join(labels, " → ")
}

func fontFace(size float64) (font.Face, error) {
	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(parsed, &opentype.FaceOptions{Size: size, DPI: 144, Hinting: font.HintingFull})
}

func fixedPoint(x, y int) fixed.Point26_6 {
	return fixed.P(x, y)
}

func encodePNG(source image.Image) ([]byte, error) {
	var output bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&output, source); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func drawLine(target *image.RGBA, x0, y0, x1, y1 int, value color.Color, thickness int) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := -1, -1
	if x0 < x1 {
		sx = 1
	}
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		for offsetX := -thickness / 2; offsetX <= thickness/2; offsetX++ {
			for offsetY := -thickness / 2; offsetY <= thickness/2; offsetY++ {
				target.Set(x0+offsetX, y0+offsetY, value)
			}
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		double := 2 * err
		if double >= dy {
			err += dy
			x0 += sx
		}
		if double <= dx {
			err += dx
			y0 += sy
		}
	}
}

func drawArrow(target *image.RGBA, from, to image.Point, value color.Color) {
	if abs(to.X-from.X) >= abs(to.Y-from.Y) {
		direction := 1
		if to.X < from.X {
			direction = -1
		}
		drawLine(target, to.X, to.Y, to.X-direction*24, to.Y-16, value, 5)
		drawLine(target, to.X, to.Y, to.X-direction*24, to.Y+16, value, 5)
		return
	}
	direction := 1
	if to.Y < from.Y {
		direction = -1
	}
	drawLine(target, to.X, to.Y, to.X-16, to.Y-direction*24, value, 5)
	drawLine(target, to.X, to.Y, to.X+16, to.Y-direction*24, value, 5)
}

func drawBorder(target *image.RGBA, rectangle image.Rectangle, value color.Color, thickness int) {
	draw.Draw(target, image.Rect(rectangle.Min.X, rectangle.Min.Y, rectangle.Max.X, rectangle.Min.Y+thickness), image.NewUniform(value), image.Point{}, draw.Src)
	draw.Draw(target, image.Rect(rectangle.Min.X, rectangle.Max.Y-thickness, rectangle.Max.X, rectangle.Max.Y), image.NewUniform(value), image.Point{}, draw.Src)
	draw.Draw(target, image.Rect(rectangle.Min.X, rectangle.Min.Y, rectangle.Min.X+thickness, rectangle.Max.Y), image.NewUniform(value), image.Point{}, draw.Src)
	draw.Draw(target, image.Rect(rectangle.Max.X-thickness, rectangle.Min.Y, rectangle.Max.X, rectangle.Max.Y), image.NewUniform(value), image.Point{}, draw.Src)
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
