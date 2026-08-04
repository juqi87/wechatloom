package workspace_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wechatloom/wechatloom/internal/workspace"
)

func TestResolveLoadsShareableProjectConfiguration(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	local := workspace.NewLocal()
	if _, err := local.Init(context.Background(), projectDir); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	config := []byte(`schema_version: "1"
project:
  name: portable-newsroom
build:
  theme: minimal
  output_dir: .wechatloom/builds
`)
	configPath := filepath.Join(projectDir, ".wechatloom", "project.yaml")
	if err := os.WriteFile(configPath, config, 0o644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	resolved, err := local.Resolve(context.Background(), projectDir)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Config.Project.Name != "portable-newsroom" {
		t.Errorf("project name = %q, want %q", resolved.Config.Project.Name, "portable-newsroom")
	}
	if resolved.Config.Build.Theme != "minimal" {
		t.Errorf("build theme = %q, want %q", resolved.Config.Build.Theme, "minimal")
	}
}

func TestSupersededLockOwnerCannotRemoveAReplacementLock(t *testing.T) {
	projectDir := t.TempDir()
	local := workspace.NewLocal()
	resolved, err := local.Init(context.Background(), projectDir)
	if err != nil {
		t.Fatalf("init workspace: %v", err)
	}
	release, err := local.LockArticle(context.Background(), projectDir, "article")
	if err != nil {
		t.Fatalf("acquire article lock: %v", err)
	}
	locks, err := filepath.Glob(filepath.Join(resolved.StatePath, "locks", "*.lock"))
	if err != nil || len(locks) != 1 {
		t.Fatalf("active locks = %v, err=%v", locks, err)
	}
	retired := locks[0] + ".retired"
	if err := os.Rename(locks[0], retired); err != nil {
		t.Fatalf("retire original lock: %v", err)
	}
	if err := os.Mkdir(locks[0], 0o700); err != nil {
		t.Fatalf("create replacement lock: %v", err)
	}
	release()
	if _, err := os.Stat(locks[0]); err != nil {
		t.Fatalf("original owner removed replacement during acquisition: %v", err)
	}
	if err := os.WriteFile(filepath.Join(locks[0], "owner-replacement"), []byte("replacement\n"), 0o600); err != nil {
		t.Fatalf("write replacement owner: %v", err)
	}
	if _, err := os.Stat(filepath.Join(locks[0], "owner-replacement")); err != nil {
		t.Fatalf("original owner removed replacement lock: %v", err)
	}
}
