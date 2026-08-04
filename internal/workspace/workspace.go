package workspace

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"
)

const projectConfig = `schema_version: "1"
project:
  name: wechatloom-project
build:
  theme: minimal
  output_dir: .wechatloom/builds
`

var ignoredWorkspacePaths = []string{
	".wechatloom/builds/",
	".wechatloom/state/",
}

type Resolved struct {
	Root       string        `json:"root"`
	ConfigPath string        `json:"config_path"`
	BuildsPath string        `json:"builds_path"`
	StatePath  string        `json:"state_path"`
	Config     ProjectConfig `json:"config"`
}

type ProjectConfig struct {
	SchemaVersion string        `json:"schema_version" yaml:"schema_version"`
	Project       Project       `json:"project" yaml:"project"`
	Build         BuildDefaults `json:"build" yaml:"build"`
}

type Project struct {
	Name string `json:"name" yaml:"name"`
}

type BuildDefaults struct {
	Theme     string `json:"theme" yaml:"theme"`
	OutputDir string `json:"output_dir" yaml:"output_dir"`
}

type BuildRecord struct {
	ID          string
	Files       map[string][]byte
	Directories []string
}

type CommittedBuild struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

type CleanResult struct {
	Removed int    `json:"removed"`
	Path    string `json:"path"`
}

type Workspace interface {
	Init(context.Context, string) (Resolved, error)
	Resolve(context.Context, string) (Resolved, error)
	CommitBuild(context.Context, string, BuildRecord) (CommittedBuild, error)
}

// Local owns WeChatLoom's on-disk project boundary.
type Local struct{}

var _ Workspace = (*Local)(nil)

func NewLocal() *Local {
	return &Local{}
}

func (local *Local) Init(ctx context.Context, root string) (Resolved, error) {
	if err := ctx.Err(); err != nil {
		return Resolved{}, err
	}

	resolved, err := paths(root)
	if err != nil {
		return Resolved{}, err
	}

	if err := os.MkdirAll(resolved.BuildsPath, 0o755); err != nil {
		return Resolved{}, fmt.Errorf("create builds directory: %w", err)
	}
	if err := os.MkdirAll(resolved.StatePath, 0o700); err != nil {
		return Resolved{}, fmt.Errorf("create state directory: %w", err)
	}

	if err := createIfMissing(resolved.ConfigPath, []byte(projectConfig), 0o644); err != nil {
		return Resolved{}, fmt.Errorf("create project config: %w", err)
	}
	if err := updateGitignore(filepath.Join(resolved.Root, ".gitignore")); err != nil {
		return Resolved{}, fmt.Errorf("update .gitignore: %w", err)
	}

	return local.Resolve(ctx, root)
}

func (local *Local) Resolve(ctx context.Context, root string) (Resolved, error) {
	if err := ctx.Err(); err != nil {
		return Resolved{}, err
	}

	resolved, err := paths(root)
	if err != nil {
		return Resolved{}, err
	}
	if _, err := os.Stat(resolved.ConfigPath); err != nil {
		return Resolved{}, fmt.Errorf("resolve project config: %w", err)
	}
	config, err := loadProjectConfig(resolved.ConfigPath)
	if err != nil {
		return Resolved{}, err
	}
	resolved.Config = config
	return resolved, nil
}

func loadProjectConfig(path string) (ProjectConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return ProjectConfig{}, fmt.Errorf("open project config: %w", err)
	}
	defer file.Close()

	var config ProjectConfig
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return ProjectConfig{}, fmt.Errorf("decode project config: %w", err)
	}
	if config.SchemaVersion != "1" {
		return ProjectConfig{}, fmt.Errorf("project config schema_version must be %q", "1")
	}
	if strings.TrimSpace(config.Project.Name) == "" {
		return ProjectConfig{}, errors.New("project config project.name is required")
	}
	if strings.TrimSpace(config.Build.Theme) == "" {
		return ProjectConfig{}, errors.New("project config build.theme is required")
	}
	if config.Build.OutputDir != ".wechatloom/builds" {
		return ProjectConfig{}, errors.New("project config build.output_dir must be .wechatloom/builds")
	}
	return config, nil
}

func (local *Local) CommitBuild(
	ctx context.Context,
	root string,
	build BuildRecord,
) (CommittedBuild, error) {
	if err := ctx.Err(); err != nil {
		return CommittedBuild{}, err
	}
	if strings.TrimSpace(build.ID) == "" {
		return CommittedBuild{}, errors.New("build ID is required")
	}

	resolved, err := local.Resolve(ctx, root)
	if err != nil {
		return CommittedBuild{}, err
	}
	finalPath := filepath.Join(resolved.BuildsPath, build.ID)
	if _, err := os.Stat(finalPath); err == nil {
		return CommittedBuild{}, fmt.Errorf("build %q already exists", build.ID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return CommittedBuild{}, err
	}

	stagingPath, err := os.MkdirTemp(resolved.BuildsPath, ".staging-*")
	if err != nil {
		return CommittedBuild{}, fmt.Errorf("create build staging directory: %w", err)
	}
	removeStaging := true
	defer func() {
		if removeStaging {
			_ = os.RemoveAll(stagingPath)
		}
	}()

	for _, directory := range build.Directories {
		target, err := safeBuildPath(stagingPath, directory)
		if err != nil {
			return CommittedBuild{}, err
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			return CommittedBuild{}, fmt.Errorf("create build directory %q: %w", directory, err)
		}
	}

	names := make([]string, 0, len(build.Files))
	for name := range build.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		target, err := safeBuildPath(stagingPath, name)
		if err != nil {
			return CommittedBuild{}, err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return CommittedBuild{}, fmt.Errorf("create parent directory for %q: %w", name, err)
		}
		if err := os.WriteFile(target, build.Files[name], 0o644); err != nil {
			return CommittedBuild{}, fmt.Errorf("write build file %q: %w", name, err)
		}
	}

	if err := os.Rename(stagingPath, finalPath); err != nil {
		return CommittedBuild{}, fmt.Errorf("commit build: %w", err)
	}
	removeStaging = false
	return CommittedBuild{ID: build.ID, Path: finalPath}, nil
}

func (local *Local) CleanBuilds(ctx context.Context, root string) (CleanResult, error) {
	if err := ctx.Err(); err != nil {
		return CleanResult{}, err
	}
	resolved, err := local.Resolve(ctx, root)
	if err != nil {
		return CleanResult{}, err
	}
	entries, err := os.ReadDir(resolved.BuildsPath)
	if err != nil {
		return CleanResult{}, fmt.Errorf("read builds directory: %w", err)
	}
	removed := 0
	for _, entry := range entries {
		target := filepath.Join(resolved.BuildsPath, entry.Name())
		if filepath.Dir(target) != resolved.BuildsPath {
			return CleanResult{}, errors.New("refusing to clean a path outside the builds directory")
		}
		if err := os.RemoveAll(target); err != nil {
			return CleanResult{}, fmt.Errorf("remove build artifact %q: %w", entry.Name(), err)
		}
		removed++
	}
	return CleanResult{Removed: removed, Path: resolved.BuildsPath}, nil
}

func (local *Local) LockArticle(ctx context.Context, root, identity string) (func(), error) {
	if strings.TrimSpace(identity) == "" {
		return nil, errors.New("article lock identity is required")
	}
	resolved, err := local.Resolve(ctx, root)
	if err != nil {
		return nil, err
	}
	lockRoot := filepath.Join(resolved.StatePath, "locks")
	if err := os.MkdirAll(lockRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	sum := sha256.Sum256([]byte(identity))
	lockPath := filepath.Join(lockRoot, hex.EncodeToString(sum[:16])+".lock")
	ownerBytes := make([]byte, 16)
	if _, err := rand.Read(ownerBytes); err != nil {
		return nil, fmt.Errorf("create article lock owner: %w", err)
	}
	ownerName := "owner-" + hex.EncodeToString(ownerBytes)
	ownerPath := filepath.Join(lockPath, ownerName)
	waitContext := ctx
	cancel := func() {}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		waitContext, cancel = context.WithTimeout(ctx, 30*time.Second)
	}
	defer cancel()
	for {
		if err := os.Mkdir(lockPath, 0o700); err == nil {
			if err := os.WriteFile(ownerPath, []byte(ownerName+"\n"), 0o600); err != nil {
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("record article lock owner: %w", err)
			}
			heartbeatDone := make(chan struct{})
			go func() {
				ticker := time.NewTicker(time.Minute)
				defer ticker.Stop()
				for {
					select {
					case now := <-ticker.C:
						if _, err := os.Stat(ownerPath); err != nil {
							return
						}
						_ = os.Chtimes(lockPath, now, now)
					case <-heartbeatDone:
						return
					}
				}
			}()
			var releaseOnce sync.Once
			return func() {
				releaseOnce.Do(func() {
					close(heartbeatDone)
					if err := os.Remove(ownerPath); err == nil {
						_ = os.Remove(lockPath)
					}
				})
			}, nil
		} else if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("acquire article lock: %w", err)
		}
		if info, err := os.Stat(lockPath); err == nil && time.Since(info.ModTime()) > 15*time.Minute {
			stalePath := lockPath + ".stale-" + ownerName
			if err := os.Rename(lockPath, stalePath); err == nil {
				_ = os.RemoveAll(stalePath)
				continue
			}
			if _, err := os.Stat(lockPath); errors.Is(err, os.ErrNotExist) {
				continue
			}
		}
		select {
		case <-waitContext.Done():
			return nil, errors.New("ARTICLE_LOCK_TIMEOUT: another draft operation is still active")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func safeBuildPath(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("build path %q must be relative", relative)
	}
	clean := filepath.Clean(relative)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("build path %q escapes the build directory", relative)
	}
	return filepath.Join(root, clean), nil
}

func paths(root string) (Resolved, error) {
	if strings.TrimSpace(root) == "" {
		return Resolved{}, errors.New("workspace root is required")
	}

	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return Resolved{}, fmt.Errorf("resolve workspace root: %w", err)
	}

	workspacePath := filepath.Join(absoluteRoot, ".wechatloom")
	return Resolved{
		Root:       absoluteRoot,
		ConfigPath: filepath.Join(workspacePath, "project.yaml"),
		BuildsPath: filepath.Join(workspacePath, "builds"),
		StatePath:  filepath.Join(workspacePath, "state"),
	}, nil
}

func createIfMissing(path string, content []byte, mode os.FileMode) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return atomicWrite(path, content, mode)
}

func updateGitignore(path string) error {
	content, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	text := string(content)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}

	changed := false
	for _, entry := range ignoredWorkspacePaths {
		if hasLine(text, entry) {
			continue
		}
		text += entry + "\n"
		changed = true
	}
	if !changed && err == nil {
		return nil
	}

	return atomicWrite(path, []byte(text), 0o644)
}

func hasLine(text, want string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func atomicWrite(path string, content []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}

	temporary, err := os.CreateTemp(directory, ".wechatloom-write-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.ReadFrom(bytes.NewReader(content)); err != nil {
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
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	removeTemporary = false
	return nil
}
