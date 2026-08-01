package builder

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var localImageReference = regexp.MustCompile(`(?:\./|\.\./)[^\s"')]+\.(?:png|jpe?g|gif|webp)`)

func resolveLocalImages(body []byte, sourceDirectory string) richMediaResult {
	markdown := string(body)
	assets := map[string][]byte{}
	diagnostics := make([]Diagnostic, 0)
	replacements := map[string]string{}
	for _, reference := range localImageReference.FindAllString(markdown, -1) {
		if _, processed := replacements[reference]; processed {
			continue
		}
		path := filepath.Join(sourceDirectory, filepath.FromSlash(reference))
		content, err := os.ReadFile(path)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{
				Code: "LOCAL_IMAGE_NOT_FOUND", Message: fmt.Sprintf("read local image %q: %v", reference, err), Line: lineOfReference(markdown, reference),
			})
			continue
		}
		mediaType := strings.Split(http.DetectContentType(content), ";")[0]
		extension := map[string]string{
			"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp",
		}[mediaType]
		if extension == "" {
			diagnostics = append(diagnostics, Diagnostic{
				Code: "LOCAL_IMAGE_INVALID", Message: fmt.Sprintf("local image %q has unsupported content type %q", reference, mediaType), Line: lineOfReference(markdown, reference),
			})
			continue
		}
		sum := sha256.Sum256(content)
		assetPath := fmt.Sprintf("assets/image-%x%s", sum[:8], extension)
		assets[assetPath] = content
		replacements[reference] = assetPath
	}
	for reference, assetPath := range replacements {
		markdown = strings.ReplaceAll(markdown, reference, assetPath)
	}
	return richMediaResult{Markdown: []byte(markdown), Assets: assets, Diagnostics: diagnostics}
}

func lineOfReference(source, reference string) int {
	index := strings.Index(source, reference)
	if index < 0 {
		return 0
	}
	return strings.Count(source[:index], "\n") + 1
}
