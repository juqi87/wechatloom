package builder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/wechatloom/wechatloom/internal/workspace"
)

type previewReceipt struct {
	SchemaVersion string    `json:"schema_version"`
	BuildID       string    `json:"build_id"`
	ContentHash   string    `json:"content_hash"`
	PreviewHash   string    `json:"preview_hash"`
	CompletedAt   time.Time `json:"completed_at"`
}

func MarkPreviewed(buildPath string) error {
	manifest, previewHash, err := previewIdentity(buildPath)
	if err != nil {
		return err
	}
	receipt := previewReceipt{
		SchemaVersion: "1", BuildID: manifest.BuildID, ContentHash: manifest.ContentHash,
		PreviewHash: previewHash, CompletedAt: time.Now().UTC(),
	}
	content, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return fmt.Errorf("encode preview receipt: %w", err)
	}
	temporary, err := os.CreateTemp(buildPath, ".preview-receipt-*")
	if err != nil {
		return fmt.Errorf("stage preview receipt: %w", err)
	}
	temporaryPath := temporary.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(content, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, filepath.Join(buildPath, "preview.receipt.json")); err != nil {
		return err
	}
	remove = false
	return nil
}

func ValidatePreviewed(buildPath string) error {
	content, err := os.ReadFile(filepath.Join(buildPath, "preview.receipt.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("DRAFT_PREVIEW_REQUIRED: open the preview or complete snapshots before planning a draft")
		}
		return fmt.Errorf("read preview receipt: %w", err)
	}
	var receipt previewReceipt
	if err := json.Unmarshal(content, &receipt); err != nil {
		return fmt.Errorf("DRAFT_PREVIEW_INVALID: decode receipt: %w", err)
	}
	manifest, previewHash, err := previewIdentity(buildPath)
	if err != nil {
		return err
	}
	if receipt.SchemaVersion != "1" || receipt.BuildID != manifest.BuildID || receipt.ContentHash != manifest.ContentHash || receipt.PreviewHash != previewHash {
		return errors.New("DRAFT_PREVIEW_CHANGED: preview receipt no longer matches the build")
	}
	return nil
}

func FindPreviewedBuild(ctx context.Context, workspaceRoot, sourcePath string) (string, error) {
	resolved, err := workspace.NewLocal().Resolve(ctx, workspaceRoot)
	if err != nil {
		return "", err
	}
	inspection, err := New().Inspect(ctx, InspectRequest{SourcePath: sourcePath})
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(resolved.BuildsPath)
	if err != nil {
		return "", fmt.Errorf("read builds directory: %w", err)
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() > entries[right].Name() })
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		buildPath := filepath.Join(resolved.BuildsPath, entry.Name())
		var manifest struct {
			Source struct {
				Path   string `json:"path"`
				SHA256 string `json:"sha256"`
			} `json:"source"`
		}
		content, err := os.ReadFile(filepath.Join(buildPath, "manifest.json"))
		if err != nil || json.Unmarshal(content, &manifest) != nil {
			continue
		}
		if filepath.Clean(manifest.Source.Path) != filepath.Clean(inspection.SourcePath) || manifest.Source.SHA256 != inspection.SourceHash {
			continue
		}
		if ValidatePreviewed(buildPath) == nil {
			return buildPath, nil
		}
	}
	return "", errors.New("DRAFT_PREVIEW_REQUIRED: build and preview the current source before creating a draft plan")
}

type previewManifest struct {
	BuildID     string `json:"build_id"`
	ContentHash string `json:"content_hash"`
}

func previewIdentity(buildPath string) (previewManifest, string, error) {
	var manifest previewManifest
	content, err := os.ReadFile(filepath.Join(buildPath, "manifest.json"))
	if err != nil {
		return manifest, "", fmt.Errorf("read preview manifest: %w", err)
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return manifest, "", fmt.Errorf("decode preview manifest: %w", err)
	}
	preview, err := os.ReadFile(filepath.Join(buildPath, "preview.html"))
	if err != nil {
		return manifest, "", fmt.Errorf("read preview HTML: %w", err)
	}
	sum := sha256.Sum256(preview)
	return manifest, hex.EncodeToString(sum[:]), nil
}
