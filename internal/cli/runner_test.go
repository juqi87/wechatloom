package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wechatloom/wechatloom/internal/catalog"
	"github.com/wechatloom/wechatloom/internal/cli"
	"github.com/wechatloom/wechatloom/internal/workspace"
)

func TestCapabilitiesJSONDescribesTheStableCLIContract(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := cli.NewRunner(&stdout, &stderr)

	exitCode := runner.Run([]string{"capabilities", "--json"})

	if exitCode != 0 {
		t.Fatalf("Run() exit code = %d, want 0; stderr = %q", exitCode, stderr.String())
	}

	var got struct {
		Success       bool            `json:"success"`
		Code          string          `json:"code"`
		SchemaVersion string          `json:"schema_version"`
		Status        string          `json:"status"`
		Retryable     bool            `json:"retryable"`
		Data          json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}

	if !got.Success {
		t.Error("success = false, want true")
	}
	if got.Code != "CAPABILITIES_READY" {
		t.Errorf("code = %q, want %q", got.Code, "CAPABILITIES_READY")
	}
	if got.SchemaVersion != "1.0" {
		t.Errorf("schema_version = %q, want %q", got.SchemaVersion, "1.0")
	}
	if got.Status != "ready" {
		t.Errorf("status = %q, want %q", got.Status, "ready")
	}
	if got.Retryable {
		t.Error("retryable = true, want false")
	}

	var data struct {
		Commands []string `json:"commands"`
		Tool     struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"tool"`
		Themes []struct {
			Name string `json:"name"`
		} `json:"themes"`
		Components []struct {
			Name          string `json:"name"`
			SchemaVersion string `json:"schema_version"`
		} `json:"components"`
		RemoteWrites struct {
			WeChatDraft *bool `json:"wechat_draft"`
		} `json:"remote_writes"`
	}
	if err := json.Unmarshal(got.Data, &data); err != nil {
		t.Fatalf("data is not valid JSON: %v", err)
	}
	if !contains(data.Commands, "build") {
		t.Errorf("commands = %v, want build capability", data.Commands)
	}
	if data.Tool.Name != "wechatloom" || data.Tool.Version == "" {
		t.Errorf("tool = %+v, want a named CLI version", data.Tool)
	}
	if len(data.Themes) != 24 || !hasNamedTheme(data.Themes, "minimal") || !hasNamedTheme(data.Themes, "tech-cyan") {
		t.Errorf("themes = %+v, want 24 built-in themes including minimal and tech-cyan", data.Themes)
	}
	if len(data.Components) != 24 || !hasNamedComponent(data.Components, "callout") ||
		!hasNamedComponent(data.Components, "hero") {
		t.Errorf("components = %+v, want 24 versioned components", data.Components)
	}
	if data.RemoteWrites.WeChatDraft == nil || *data.RemoteWrites.WeChatDraft {
		t.Errorf("wechat_draft remote write = %v, want explicit false in v0.1", data.RemoteWrites.WeChatDraft)
	}
}

func TestSkillStatusReportsAMissingCodexInstallationWithoutWriting(t *testing.T) {
	t.Parallel()

	codexHome := t.TempDir()
	var stdout, stderr bytes.Buffer
	exitCode := cli.NewRunner(&stdout, &stderr).Run([]string{
		"skill", "status", "codex", "--codex-home", codexHome, "--json",
	})
	if exitCode != 0 {
		t.Fatalf("skill status exit code = %d; stderr = %q; stdout = %q", exitCode, stderr.String(), stdout.String())
	}
	var response struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Status  string `json:"status"`
		Data    struct {
			Target        string `json:"target"`
			State         string `json:"state"`
			Installed     bool   `json:"installed"`
			Path          string `json:"path"`
			SourceVersion string `json:"source_version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode skill status response: %v\n%s", err, stdout.String())
	}
	wantPath := filepath.Join(codexHome, "skills", "wechatloom")
	if !response.Success || response.Code != "SKILL_STATUS_READY" || response.Status != "ready" {
		t.Errorf("response = %+v, want ready skill status", response)
	}
	if response.Data.Target != "codex" || response.Data.State != "not_installed" || response.Data.Installed {
		t.Errorf("skill state = %+v, want missing Codex installation", response.Data)
	}
	if response.Data.Path != wantPath || response.Data.SourceVersion != "0.3.0-dev" {
		t.Errorf("skill metadata = %+v, want path %q and source version 0.3.0-dev", response.Data, wantPath)
	}
	if _, err := os.Stat(wantPath); !os.IsNotExist(err) {
		t.Errorf("skill status wrote target path; stat error = %v", err)
	}
}

func TestSkillInstallCreatesAPortableVersionedCodexSkill(t *testing.T) {
	t.Parallel()

	codexHome := t.TempDir()
	var installOut, installErr bytes.Buffer
	exitCode := cli.NewRunner(&installOut, &installErr).Run([]string{
		"skill", "install", "codex", "--codex-home", codexHome, "--json",
	})
	if exitCode != 0 {
		t.Fatalf("skill install exit code = %d; stderr = %q; stdout = %q", exitCode, installErr.String(), installOut.String())
	}
	var installed struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Status  string `json:"status"`
		Data    struct {
			Path             string   `json:"path"`
			InstalledVersion string   `json:"installed_version"`
			Files            []string `json:"files"`
		} `json:"data"`
	}
	if err := json.Unmarshal(installOut.Bytes(), &installed); err != nil {
		t.Fatalf("decode skill install response: %v\n%s", err, installOut.String())
	}
	targetPath := filepath.Join(codexHome, "skills", "wechatloom")
	if !installed.Success || installed.Code != "SKILL_INSTALLED" || installed.Status != "completed" {
		t.Errorf("install response = %+v, want completed installation", installed)
	}
	if installed.Data.Path != targetPath || installed.Data.InstalledVersion != "0.3.0-dev" {
		t.Errorf("install data = %+v, want path %q and version 0.3.0-dev", installed.Data, targetPath)
	}
	for _, relativePath := range []string{"SKILL.md", filepath.Join("agents", "openai.yaml"), ".wechatloom-skill.json"} {
		if info, err := os.Stat(filepath.Join(targetPath, relativePath)); err != nil || !info.Mode().IsRegular() {
			t.Errorf("installed file %q: info=%v err=%v", relativePath, info, err)
		}
	}
	manifestBytes, err := os.ReadFile(filepath.Join(targetPath, ".wechatloom-skill.json"))
	if err != nil {
		t.Fatalf("read skill manifest: %v", err)
	}
	var manifest struct {
		SchemaVersion string            `json:"schema_version"`
		Skill         string            `json:"skill"`
		Target        string            `json:"target"`
		SourceVersion string            `json:"source_version"`
		Files         map[string]string `json:"files"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("decode skill manifest: %v", err)
	}
	if manifest.SchemaVersion != "1" || manifest.Skill != "wechatloom" || manifest.Target != "codex" || manifest.SourceVersion != "0.3.0-dev" {
		t.Errorf("skill manifest = %+v, want versioned Codex source metadata", manifest)
	}
	if len(manifest.Files) != 2 || manifest.Files["SKILL.md"] == "" || manifest.Files["agents/openai.yaml"] == "" {
		t.Errorf("skill manifest files = %+v, want hashes for both portable assets", manifest.Files)
	}

	var statusOut, statusErr bytes.Buffer
	exitCode = cli.NewRunner(&statusOut, &statusErr).Run([]string{
		"skill", "status", "codex", "--codex-home", codexHome, "--json",
	})
	if exitCode != 0 {
		t.Fatalf("installed skill status exit code = %d; stderr = %q", exitCode, statusErr.String())
	}
	var status struct {
		Data struct {
			State            string `json:"state"`
			Installed        bool   `json:"installed"`
			InstalledVersion string `json:"installed_version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(statusOut.Bytes(), &status); err != nil {
		t.Fatalf("decode installed skill status: %v", err)
	}
	if status.Data.State != "installed" || !status.Data.Installed || status.Data.InstalledVersion != "0.3.0-dev" {
		t.Errorf("installed skill status = %+v, want matching installed version", status.Data)
	}
}

func TestSkillUpdateAtomicallyRestoresTheBundledCodexSkill(t *testing.T) {
	t.Parallel()

	codexHome := t.TempDir()
	var installOut, installErr bytes.Buffer
	if exitCode := cli.NewRunner(&installOut, &installErr).Run([]string{
		"skill", "install", "codex", "--codex-home", codexHome, "--json",
	}); exitCode != 0 {
		t.Fatalf("skill install exit code = %d; stderr = %q", exitCode, installErr.String())
	}
	targetPath := filepath.Join(codexHome, "skills", "wechatloom")
	skillPath := filepath.Join(targetPath, "SKILL.md")
	bundledSkill, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read installed skill: %v", err)
	}
	if err := os.WriteFile(skillPath, []byte("locally modified\n"), 0o644); err != nil {
		t.Fatalf("modify installed skill: %v", err)
	}

	var updateOut, updateErr bytes.Buffer
	exitCode := cli.NewRunner(&updateOut, &updateErr).Run([]string{
		"skill", "update", "codex", "--codex-home", codexHome, "--json",
	})
	if exitCode != 0 {
		t.Fatalf("skill update exit code = %d; stderr = %q; stdout = %q", exitCode, updateErr.String(), updateOut.String())
	}
	var response struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Status  string `json:"status"`
		Data    struct {
			PreviousVersion  string `json:"previous_version"`
			InstalledVersion string `json:"installed_version"`
			Path             string `json:"path"`
		} `json:"data"`
	}
	if err := json.Unmarshal(updateOut.Bytes(), &response); err != nil {
		t.Fatalf("decode skill update response: %v\n%s", err, updateOut.String())
	}
	if !response.Success || response.Code != "SKILL_UPDATED" || response.Status != "completed" {
		t.Errorf("update response = %+v, want completed update", response)
	}
	if response.Data.PreviousVersion != "0.3.0-dev" || response.Data.InstalledVersion != "0.3.0-dev" || response.Data.Path != targetPath {
		t.Errorf("update data = %+v, want an in-place managed update", response.Data)
	}
	updatedSkill, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read updated skill: %v", err)
	}
	if !bytes.Equal(updatedSkill, bundledSkill) {
		t.Error("skill update did not restore the bundled SKILL.md")
	}
	backups, err := filepath.Glob(filepath.Join(codexHome, "skills", ".wechatloom-backup-*"))
	if err != nil {
		t.Fatalf("glob skill backups: %v", err)
	}
	if len(backups) != 0 {
		t.Errorf("skill update left rollback directories: %v", backups)
	}
}

func TestSkillStatusDetectsLocallyModifiedManagedFiles(t *testing.T) {
	t.Parallel()

	codexHome := t.TempDir()
	var installOut, installErr bytes.Buffer
	if exitCode := cli.NewRunner(&installOut, &installErr).Run([]string{
		"skill", "install", "codex", "--codex-home", codexHome, "--json",
	}); exitCode != 0 {
		t.Fatalf("skill install exit code = %d; stderr = %q", exitCode, installErr.String())
	}
	skillPath := filepath.Join(codexHome, "skills", "wechatloom", "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("locally modified\n"), 0o644); err != nil {
		t.Fatalf("modify installed skill: %v", err)
	}

	var stdout, stderr bytes.Buffer
	exitCode := cli.NewRunner(&stdout, &stderr).Run([]string{
		"skill", "status", "codex", "--codex-home", codexHome, "--json",
	})
	if exitCode != 0 {
		t.Fatalf("skill status exit code = %d; stderr = %q", exitCode, stderr.String())
	}
	var response struct {
		Data struct {
			State            string   `json:"state"`
			Installed        bool     `json:"installed"`
			InstalledVersion string   `json:"installed_version"`
			ModifiedFiles    []string `json:"modified_files"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode skill status: %v", err)
	}
	if response.Data.State != "modified" || !response.Data.Installed || response.Data.InstalledVersion != "0.3.0-dev" {
		t.Errorf("skill status = %+v, want modified managed installation", response.Data)
	}
	if !reflect.DeepEqual(response.Data.ModifiedFiles, []string{"SKILL.md"}) {
		t.Errorf("modified files = %v, want SKILL.md", response.Data.ModifiedFiles)
	}
}

func TestThemeAndComponentDiscoveryJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		args     []string
		wantCode string
		assert   func(*testing.T, json.RawMessage)
	}{
		{
			args: []string{"theme", "list", "--json"}, wantCode: "THEMES_LISTED",
			assert: func(t *testing.T, raw json.RawMessage) {
				var data struct {
					Resources []struct {
						Name string `json:"name"`
					} `json:"resources"`
				}
				if err := json.Unmarshal(raw, &data); err != nil {
					t.Fatalf("decode theme list: %v", err)
				}
				if len(data.Resources) != 24 {
					t.Errorf("theme count = %d, want 24", len(data.Resources))
				}
			},
		},
		{
			args: []string{"theme", "show", "tech-cyan", "--json"}, wantCode: "THEME_SHOWN",
			assert: func(t *testing.T, raw json.RawMessage) {
				var data struct {
					Theme struct {
						Name    string `json:"name"`
						Version string `json:"version"`
					} `json:"theme"`
				}
				if err := json.Unmarshal(raw, &data); err != nil {
					t.Fatalf("decode theme: %v", err)
				}
				if data.Theme.Name != "tech-cyan" || data.Theme.Version != "0.2.0" {
					t.Errorf("theme = %+v", data.Theme)
				}
			},
		},
		{
			args: []string{"component", "list", "--json"}, wantCode: "COMPONENTS_LISTED",
			assert: func(t *testing.T, raw json.RawMessage) {
				var data struct {
					Resources []struct {
						Name string `json:"name"`
					} `json:"resources"`
				}
				if err := json.Unmarshal(raw, &data); err != nil {
					t.Fatalf("decode component list: %v", err)
				}
				if len(data.Resources) != 24 {
					t.Errorf("component count = %d, want 24", len(data.Resources))
				}
			},
		},
		{
			args: []string{"component", "show", "timeline", "--json"}, wantCode: "COMPONENT_SHOWN",
			assert: func(t *testing.T, raw json.RawMessage) {
				var data struct {
					Component struct {
						Name           string `json:"name"`
						ValidExample   string `json:"valid_example"`
						InvalidExample string `json:"invalid_example"`
					} `json:"component"`
				}
				if err := json.Unmarshal(raw, &data); err != nil {
					t.Fatalf("decode component: %v", err)
				}
				if data.Component.Name != "timeline" || data.Component.ValidExample == "" || data.Component.InvalidExample == "" {
					t.Errorf("component = %+v", data.Component)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(strings.Join(test.args[:2], "-"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exitCode := cli.NewRunner(&stdout, &stderr).Run(test.args)
			if exitCode != 0 {
				t.Fatalf("exit code = %d; stderr = %q", exitCode, stderr.String())
			}
			var response struct {
				Success bool            `json:"success"`
				Code    string          `json:"code"`
				Data    json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v\n%s", err, stdout.String())
			}
			if !response.Success || response.Code != test.wantCode {
				t.Fatalf("response = %+v, want %s", response, test.wantCode)
			}
			test.assert(t, response.Data)
		})
	}
}

func TestThemeExportValidateAndInstallRoundTrip(t *testing.T) {
	t.Parallel()

	exportDirectory := filepath.Join(t.TempDir(), "themes")
	var exportOut, exportErr bytes.Buffer
	exitCode := cli.NewRunner(&exportOut, &exportErr).Run([]string{
		"theme", "export", "--all", "--output", exportDirectory, "--json",
	})
	if exitCode != 0 {
		t.Fatalf("theme export exit code = %d; stderr = %q; stdout = %q", exitCode, exportErr.String(), exportOut.String())
	}
	packages, err := filepath.Glob(filepath.Join(exportDirectory, "*", "theme.json"))
	if err != nil {
		t.Fatalf("glob exported themes: %v", err)
	}
	if len(packages) != 24 {
		t.Fatalf("exported theme packages = %d, want 24", len(packages))
	}
	minimalPackage := filepath.Join(exportDirectory, "minimal", "theme.json")
	packageBytes, err := os.ReadFile(minimalPackage)
	if err != nil {
		t.Fatalf("read exported minimal package: %v", err)
	}
	var customized catalog.ThemePackage
	if err := json.Unmarshal(packageBytes, &customized); err != nil {
		t.Fatalf("decode exported minimal package: %v", err)
	}
	customized.Theme.Version = "0.2.1-project"
	packageBytes, err = json.MarshalIndent(customized, "", "  ")
	if err != nil {
		t.Fatalf("encode customized minimal package: %v", err)
	}
	if err := os.WriteFile(minimalPackage, append(packageBytes, '\n'), 0o644); err != nil {
		t.Fatalf("write customized minimal package: %v", err)
	}

	var validateOut, validateErr bytes.Buffer
	exitCode = cli.NewRunner(&validateOut, &validateErr).Run([]string{"theme", "validate", minimalPackage, "--json"})
	if exitCode != 0 {
		t.Fatalf("theme validate exit code = %d; stderr = %q", exitCode, validateErr.String())
	}
	var validation struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	if err := json.Unmarshal(validateOut.Bytes(), &validation); err != nil {
		t.Fatalf("decode validation response: %v", err)
	}
	if !validation.Success || validation.Code != "THEME_VALID" {
		t.Errorf("validation response = %+v", validation)
	}

	projectDir := t.TempDir()
	if _, err := workspace.NewLocal().Init(context.Background(), projectDir); err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	var installOut, installErr bytes.Buffer
	exitCode = cli.NewRunner(&installOut, &installErr).Run([]string{
		"theme", "install", minimalPackage, "--root", projectDir, "--json",
	})
	if exitCode != 0 {
		t.Fatalf("theme install exit code = %d; stderr = %q", exitCode, installErr.String())
	}
	installedPath := filepath.Join(projectDir, ".wechatloom", "themes", "minimal", "theme.json")
	if _, err := os.Stat(installedPath); err != nil {
		t.Errorf("stat installed theme: %v", err)
	}
	var showOut, showErr bytes.Buffer
	exitCode = cli.NewRunner(&showOut, &showErr).Run([]string{"theme", "show", "minimal", "--root", projectDir, "--json"})
	if exitCode != 0 {
		t.Fatalf("show installed theme exit code = %d; stderr = %q", exitCode, showErr.String())
	}
	var shown struct {
		Data struct {
			Theme struct {
				Version string `json:"version"`
			} `json:"theme"`
		} `json:"data"`
	}
	if err := json.Unmarshal(showOut.Bytes(), &shown); err != nil {
		t.Fatalf("decode installed theme response: %v", err)
	}
	if shown.Data.Theme.Version != "0.2.1-project" {
		t.Errorf("installed theme version = %q, want project override", shown.Data.Theme.Version)
	}
}

func TestComponentExportWritesSchemasAndValidInvalidExamples(t *testing.T) {
	t.Parallel()

	outputDirectory := t.TempDir()
	var stdout, stderr bytes.Buffer
	exitCode := cli.NewRunner(&stdout, &stderr).Run([]string{
		"component", "export", "--all", "--output", outputDirectory, "--json",
	})
	if exitCode != 0 {
		t.Fatalf("component export exit code = %d; stderr = %q", exitCode, stderr.String())
	}
	schemas, err := filepath.Glob(filepath.Join(outputDirectory, "*", "schema.json"))
	if err != nil {
		t.Fatalf("glob schemas: %v", err)
	}
	validExamples, err := filepath.Glob(filepath.Join(outputDirectory, "*", "examples", "valid.md"))
	if err != nil {
		t.Fatalf("glob valid examples: %v", err)
	}
	invalidExamples, err := filepath.Glob(filepath.Join(outputDirectory, "*", "examples", "invalid.md"))
	if err != nil {
		t.Fatalf("glob invalid examples: %v", err)
	}
	if len(schemas) != 24 || len(validExamples) != 24 || len(invalidExamples) != 24 {
		t.Errorf("export counts = schemas %d valid %d invalid %d, want 24 each", len(schemas), len(validExamples), len(invalidExamples))
	}
	heroSchema, err := os.ReadFile(filepath.Join(outputDirectory, "hero", "schema.json"))
	if err != nil {
		t.Fatalf("read hero schema: %v", err)
	}
	var schema struct {
		Schema               string   `json:"$schema"`
		AdditionalProperties bool     `json:"additionalProperties"`
		Required             []string `json:"required"`
	}
	if err := json.Unmarshal(heroSchema, &schema); err != nil {
		t.Fatalf("decode hero schema: %v", err)
	}
	if schema.Schema == "" || schema.AdditionalProperties || len(schema.Required) == 0 {
		t.Errorf("hero schema = %+v, want strict JSON Schema", schema)
	}
}

func TestInitCreatesAnIdempotentPortableWorkspace(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()

	for attempt := 0; attempt < 2; attempt++ {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := cli.NewRunner(&stdout, &stderr).Run(
			[]string{"init", projectDir, "--json"},
		)
		if exitCode != 0 {
			t.Fatalf(
				"attempt %d: Run() exit code = %d, want 0; stderr = %q",
				attempt+1,
				exitCode,
				stderr.String(),
			)
		}

		var response struct {
			Success bool   `json:"success"`
			Code    string `json:"code"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
			t.Fatalf("attempt %d: stdout is not valid JSON: %v", attempt+1, err)
		}
		if !response.Success || response.Code != "WORKSPACE_INITIALIZED" {
			t.Fatalf("attempt %d: response = %+v, want successful initialization", attempt+1, response)
		}
	}

	configPath := filepath.Join(projectDir, ".wechatloom", "project.yaml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read %s: %v", configPath, err)
	}
	if !strings.Contains(string(config), "schema_version: \"1\"") {
		t.Errorf("project config does not declare schema version:\n%s", config)
	}
	if !strings.Contains(string(config), "theme: minimal") {
		t.Errorf("project config does not declare the default theme:\n%s", config)
	}

	for _, path := range []string{
		filepath.Join(projectDir, ".wechatloom", "builds"),
		filepath.Join(projectDir, ".wechatloom", "state"),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("stat %s: %v", path, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", path)
		}
	}

	gitignore, err := os.ReadFile(filepath.Join(projectDir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	for _, entry := range []string{".wechatloom/builds/", ".wechatloom/state/"} {
		if count := strings.Count(string(gitignore), entry); count != 1 {
			t.Errorf(".gitignore contains %q %d times, want once:\n%s", entry, count, gitignore)
		}
	}
}

func TestInspectJSONReportsArticleReadiness(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := cli.NewRunner(&stdout, &stderr).Run([]string{
		"inspect",
		filepath.Join("..", "..", "testdata", "article.md"),
		"--json",
	})
	if exitCode != 0 {
		t.Fatalf("Run() exit code = %d, want 0; stderr = %q", exitCode, stderr.String())
	}

	var response struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Status  string `json:"status"`
		Data    struct {
			Title        string `json:"title"`
			CalloutCount int    `json:"callout_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}
	if !response.Success || response.Code != "INSPECTION_COMPLETED" || response.Status != "ready" {
		t.Errorf("response = %+v, want a ready inspection", response)
	}
	if response.Data.Title != "用 WeChatLoom 构建第一篇文章" || response.Data.CalloutCount != 1 {
		t.Errorf("inspection data = %+v, want article metadata", response.Data)
	}
}

func TestBuildJSONReturnsCommittedLocalArtifacts(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	if _, err := workspace.NewLocal().Init(context.Background(), projectDir); err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}

	source, err := os.ReadFile(filepath.Join("..", "..", "testdata", "article.md"))
	if err != nil {
		t.Fatalf("read source fixture: %v", err)
	}
	sourcePath := filepath.Join(projectDir, "article.md")
	if err := os.WriteFile(sourcePath, source, 0o644); err != nil {
		t.Fatalf("write project article: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := cli.NewRunner(&stdout, &stderr).Run([]string{
		"build",
		sourcePath,
		"--root",
		projectDir,
		"--theme",
		"tech-violet",
		"--json",
	})
	if exitCode != 0 {
		t.Fatalf("Run() exit code = %d, want 0; stderr = %q", exitCode, stderr.String())
	}

	var response struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Status  string `json:"status"`
		Data    struct {
			ArticleHTMLPath string `json:"article_html_path"`
			PreviewHTMLPath string `json:"preview_html_path"`
			ManifestPath    string `json:"manifest_path"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}
	if !response.Success || response.Code != "BUILD_COMPLETED" || response.Status != "completed" {
		t.Errorf("response = %+v, want a completed build", response)
	}
	for _, artifactPath := range []string{
		response.Data.ArticleHTMLPath,
		response.Data.PreviewHTMLPath,
		response.Data.ManifestPath,
	} {
		if _, err := os.Stat(artifactPath); err != nil {
			t.Errorf("stat build artifact %q: %v", artifactPath, err)
		}
	}
	articleHTML, err := os.ReadFile(response.Data.ArticleHTMLPath)
	if err != nil {
		t.Fatalf("read built article: %v", err)
	}
	if !bytes.Contains(articleHTML, []byte(`data-wechatloom-theme="tech-violet"`)) {
		t.Errorf("CLI --theme override was not applied:\n%s", articleHTML)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasNamedTheme(values []struct {
	Name string `json:"name"`
}, want string) bool {
	for _, value := range values {
		if value.Name == want {
			return true
		}
	}
	return false
}

func hasNamedComponent(values []struct {
	Name          string `json:"name"`
	SchemaVersion string `json:"schema_version"`
}, want string) bool {
	for _, value := range values {
		if value.Name == want && value.SchemaVersion == "1" {
			return true
		}
	}
	return false
}
