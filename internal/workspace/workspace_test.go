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
