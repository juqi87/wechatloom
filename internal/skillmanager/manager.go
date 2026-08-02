package skillmanager

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/wechatloom/wechatloom/internal/version"
	wechatloomskill "github.com/wechatloom/wechatloom/skills/wechatloom"
)

const manifestName = ".wechatloom-skill.json"

type Status struct {
	Target           string   `json:"target"`
	State            string   `json:"state"`
	Installed        bool     `json:"installed"`
	Path             string   `json:"path"`
	SourceVersion    string   `json:"source_version"`
	InstalledVersion string   `json:"installed_version,omitempty"`
	ModifiedFiles    []string `json:"modified_files,omitempty"`
}

type InstallResult struct {
	Target           string   `json:"target"`
	Path             string   `json:"path"`
	InstalledVersion string   `json:"installed_version"`
	Files            []string `json:"files"`
}

type UpdateResult struct {
	Target           string   `json:"target"`
	Path             string   `json:"path"`
	PreviousVersion  string   `json:"previous_version"`
	InstalledVersion string   `json:"installed_version"`
	Files            []string `json:"files"`
}

type manifest struct {
	SchemaVersion string            `json:"schema_version"`
	Skill         string            `json:"skill"`
	Target        string            `json:"target"`
	SourceVersion string            `json:"source_version"`
	Files         map[string]string `json:"files"`
}

func CodexStatus(codexHome string) (Status, error) {
	targetPath := filepath.Join(codexHome, "skills", "wechatloom")
	result := Status{
		Target: "codex", State: "not_installed", Path: targetPath, SourceVersion: version.Version,
	}
	info, err := os.Stat(filepath.Join(targetPath, "SKILL.md"))
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return Status{}, err
	}
	if !info.Mode().IsRegular() {
		return Status{}, fmt.Errorf("installed SKILL.md is not a regular file")
	}
	result.Installed = true
	result.State = "unmanaged"
	manifestBytes, err := os.ReadFile(filepath.Join(targetPath, manifestName))
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return Status{}, err
	}
	var installed manifest
	if err := json.Unmarshal(manifestBytes, &installed); err != nil {
		return Status{}, fmt.Errorf("decode installed skill manifest: %w", err)
	}
	result.InstalledVersion = installed.SourceVersion
	result.State = "installed"
	if installed.SourceVersion != version.Version {
		result.State = "outdated"
	}
	for assetPath, expectedHash := range installed.Files {
		content, err := os.ReadFile(filepath.Join(targetPath, filepath.FromSlash(assetPath)))
		if os.IsNotExist(err) {
			result.ModifiedFiles = append(result.ModifiedFiles, assetPath)
			continue
		}
		if err != nil {
			return Status{}, err
		}
		sum := sha256.Sum256(content)
		if fmt.Sprintf("%x", sum) != expectedHash {
			result.ModifiedFiles = append(result.ModifiedFiles, assetPath)
		}
	}
	if len(result.ModifiedFiles) != 0 {
		sort.Strings(result.ModifiedFiles)
		result.State = "modified"
	}
	return result, nil
}

func InstallCodex(ctx context.Context, codexHome string) (InstallResult, error) {
	if err := ctx.Err(); err != nil {
		return InstallResult{}, err
	}
	targetPath := filepath.Join(codexHome, "skills", "wechatloom")
	if _, err := os.Stat(targetPath); err == nil {
		return InstallResult{}, fmt.Errorf("Codex skill already exists at %s; use skill update codex", targetPath)
	} else if !os.IsNotExist(err) {
		return InstallResult{}, err
	}
	parent := filepath.Dir(targetPath)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("create Codex skills directory: %w", err)
	}
	stagingPath, installedFiles, err := stageCodex(ctx, parent)
	if err != nil {
		return InstallResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(stagingPath)
		}
	}()
	if err := os.Rename(stagingPath, targetPath); err != nil {
		return InstallResult{}, fmt.Errorf("commit Codex skill installation: %w", err)
	}
	committed = true
	return InstallResult{
		Target: "codex", Path: targetPath, InstalledVersion: version.Version, Files: installedFiles,
	}, nil
}

func UpdateCodex(ctx context.Context, codexHome string) (UpdateResult, error) {
	if err := ctx.Err(); err != nil {
		return UpdateResult{}, err
	}
	targetPath := filepath.Join(codexHome, "skills", "wechatloom")
	installed, err := readManagedManifest(targetPath)
	if err != nil {
		return UpdateResult{}, err
	}
	parent := filepath.Dir(targetPath)
	stagingPath, installedFiles, err := stageCodex(ctx, parent)
	if err != nil {
		return UpdateResult{}, err
	}
	stagingCommitted := false
	defer func() {
		if !stagingCommitted {
			_ = os.RemoveAll(stagingPath)
		}
	}()
	backupPath, err := os.MkdirTemp(parent, ".wechatloom-backup-*")
	if err != nil {
		return UpdateResult{}, fmt.Errorf("reserve skill rollback path: %w", err)
	}
	if err := os.Remove(backupPath); err != nil {
		return UpdateResult{}, fmt.Errorf("prepare skill rollback path: %w", err)
	}
	if err := os.Rename(targetPath, backupPath); err != nil {
		return UpdateResult{}, fmt.Errorf("stage installed Codex skill for rollback: %w", err)
	}
	if err := os.Rename(stagingPath, targetPath); err != nil {
		rollbackErr := os.Rename(backupPath, targetPath)
		if rollbackErr != nil {
			return UpdateResult{}, fmt.Errorf("commit Codex skill update: %w; rollback also failed: %v", err, rollbackErr)
		}
		return UpdateResult{}, fmt.Errorf("commit Codex skill update: %w", err)
	}
	stagingCommitted = true
	if err := os.RemoveAll(backupPath); err != nil {
		return UpdateResult{}, fmt.Errorf("remove Codex skill rollback directory: %w", err)
	}
	return UpdateResult{
		Target:           "codex",
		Path:             targetPath,
		PreviousVersion:  installed.SourceVersion,
		InstalledVersion: version.Version,
		Files:            installedFiles,
	}, nil
}

func readManagedManifest(targetPath string) (manifest, error) {
	metadata, err := os.ReadFile(filepath.Join(targetPath, manifestName))
	if os.IsNotExist(err) {
		return manifest{}, fmt.Errorf("Codex skill at %s is not managed by WeChatLoom", targetPath)
	}
	if err != nil {
		return manifest{}, err
	}
	var installed manifest
	if err := json.Unmarshal(metadata, &installed); err != nil {
		return manifest{}, fmt.Errorf("decode installed skill manifest: %w", err)
	}
	if installed.SchemaVersion != "1" || installed.Skill != "wechatloom" || installed.Target != "codex" {
		return manifest{}, fmt.Errorf("Codex skill manifest is not a supported WeChatLoom installation")
	}
	return installed, nil
}

func stageCodex(ctx context.Context, parent string) (string, []string, error) {
	stagingPath, err := os.MkdirTemp(parent, ".wechatloom-install-*")
	if err != nil {
		return "", nil, fmt.Errorf("create skill staging directory: %w", err)
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = os.RemoveAll(stagingPath)
		}
	}()
	fileHashes := make(map[string]string, len(wechatloomskill.Files))
	installedFiles := make([]string, 0, len(wechatloomskill.Files)+1)
	for _, assetPath := range wechatloomskill.Files {
		if err := ctx.Err(); err != nil {
			return "", nil, err
		}
		content, err := fs.ReadFile(wechatloomskill.Assets, assetPath)
		if err != nil {
			return "", nil, fmt.Errorf("read bundled skill asset %q: %w", assetPath, err)
		}
		destination := filepath.Join(stagingPath, filepath.FromSlash(assetPath))
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return "", nil, fmt.Errorf("create skill asset directory: %w", err)
		}
		if err := os.WriteFile(destination, content, 0o644); err != nil {
			return "", nil, fmt.Errorf("write skill asset %q: %w", assetPath, err)
		}
		sum := sha256.Sum256(content)
		fileHashes[assetPath] = fmt.Sprintf("%x", sum)
		installedFiles = append(installedFiles, assetPath)
	}
	metadata, err := json.MarshalIndent(manifest{
		SchemaVersion: "1",
		Skill:         "wechatloom",
		Target:        "codex",
		SourceVersion: version.Version,
		Files:         fileHashes,
	}, "", "  ")
	if err != nil {
		return "", nil, fmt.Errorf("encode skill manifest: %w", err)
	}
	metadata = append(metadata, '\n')
	if err := os.WriteFile(filepath.Join(stagingPath, manifestName), metadata, 0o644); err != nil {
		return "", nil, fmt.Errorf("write skill manifest: %w", err)
	}
	installedFiles = append(installedFiles, manifestName)
	succeeded = true
	return stagingPath, installedFiles, nil
}
