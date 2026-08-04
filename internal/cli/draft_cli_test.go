package cli_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wechatloom/wechatloom/internal/builder"
	"github.com/wechatloom/wechatloom/internal/cli"
	"github.com/wechatloom/wechatloom/internal/publisher"
	"github.com/wechatloom/wechatloom/internal/workspace"
)

type noRemoteDraftPort struct{}

func buildPreviewedForCLI(ctx context.Context, request builder.BuildRequest) (builder.BuildResult, error) {
	result, err := builder.New().Build(ctx, request)
	if err != nil {
		return builder.BuildResult{}, err
	}
	if err := builder.MarkPreviewed(result.BuildPath); err != nil {
		return builder.BuildResult{}, err
	}
	return result, nil
}

func (noRemoteDraftPort) AccessToken(context.Context, publisher.AccountCredentials) (publisher.Token, error) {
	panic("dry-run contacted WeChat")
}
func (noRemoteDraftPort) UploadContentImage(context.Context, publisher.Token, publisher.MediaFile) (publisher.ContentImage, error) {
	panic("dry-run uploaded content")
}
func (noRemoteDraftPort) UploadCover(context.Context, publisher.Token, publisher.MediaFile) (publisher.CoverMedia, error) {
	panic("dry-run uploaded cover")
}
func (noRemoteDraftPort) AddDraft(context.Context, publisher.Token, publisher.WeChatDraft) (publisher.RemoteDraft, error) {
	panic("dry-run added draft")
}
func (noRemoteDraftPort) UpdateDraft(context.Context, publisher.Token, publisher.WeChatDraftUpdate) error {
	panic("dry-run updated draft")
}

type confirmedDraftPort struct{}

func (confirmedDraftPort) AccessToken(context.Context, publisher.AccountCredentials) (publisher.Token, error) {
	return publisher.Token{Value: "token", ExpiresIn: 7200}, nil
}
func (confirmedDraftPort) UploadContentImage(context.Context, publisher.Token, publisher.MediaFile) (publisher.ContentImage, error) {
	return publisher.ContentImage{URL: "https://mmbiz.qpic.cn/content"}, nil
}
func (confirmedDraftPort) UploadCover(context.Context, publisher.Token, publisher.MediaFile) (publisher.CoverMedia, error) {
	return publisher.CoverMedia{MediaID: "cover"}, nil
}
func (confirmedDraftPort) AddDraft(context.Context, publisher.Token, publisher.WeChatDraft) (publisher.RemoteDraft, error) {
	return publisher.RemoteDraft{MediaID: "draft-media-id"}, nil
}
func (confirmedDraftPort) UpdateDraft(context.Context, publisher.Token, publisher.WeChatDraftUpdate) error {
	return nil
}

func TestDraftDryRunReturnsExplicitConfirmationPlanWithoutRemoteWrites(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	if _, err := workspace.NewLocal().Init(ctx, projectRoot); err != nil {
		t.Fatalf("init workspace: %v", err)
	}
	sourcePath := filepath.Join(projectRoot, "article.md")
	if err := os.WriteFile(sourcePath, []byte("---\ntitle: CLI draft\n---\n\n# CLI draft\n"), 0o644); err != nil {
		t.Fatalf("write article: %v", err)
	}
	built, err := buildPreviewedForCLI(ctx, builder.BuildRequest{WorkspaceRoot: projectRoot, SourcePath: sourcePath})
	if err != nil {
		t.Fatalf("build previewed article: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("schema_version: \"1\"\nwechat:\n  default_account: personal\n  accounts:\n    personal:\n      app_id: wx1234567890abcdef\n      app_secret: test-secret\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	coverBytes, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	coverPath := filepath.Join(projectRoot, "cover.png")
	if err := os.WriteFile(coverPath, coverBytes, 0o644); err != nil {
		t.Fatalf("write cover: %v", err)
	}

	var stdout, stderr bytes.Buffer
	runner := cli.NewRunnerWithPublisher(&stdout, &stderr, publisher.NewService(noRemoteDraftPort{}))
	exitCode := runner.Run([]string{"draft", built.BuildPath, "--root", projectRoot, "--config", configPath, "--cover", coverPath, "--dry-run", "--json"})
	if exitCode != 0 {
		t.Fatalf("dry-run exit = %d; stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	var response struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Data    struct {
			Operation         string `json:"operation"`
			Account           string `json:"account"`
			ConfirmationToken string `json:"confirmation_token"`
			PlanPath          string `json:"plan_path"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v; %s", err, stdout.String())
	}
	if !response.Success || response.Code != "DRAFT_PLAN_READY" || response.Data.Operation != "add" || response.Data.Account != "personal" || response.Data.ConfirmationToken == "" || response.Data.PlanPath == "" {
		t.Errorf("dry-run response = %+v", response)
	}
}

func TestDraftConfirmSubmitsOnlyThePersistedExplicitPlan(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	if _, err := workspace.NewLocal().Init(ctx, projectRoot); err != nil {
		t.Fatalf("init workspace: %v", err)
	}
	sourcePath := filepath.Join(projectRoot, "article.md")
	if err := os.WriteFile(sourcePath, []byte("---\ntitle: Confirmed CLI draft\n---\n\n# Confirmed CLI draft\n"), 0o644); err != nil {
		t.Fatalf("write article: %v", err)
	}
	built, err := buildPreviewedForCLI(ctx, builder.BuildRequest{WorkspaceRoot: projectRoot, SourcePath: sourcePath})
	if err != nil {
		t.Fatalf("build article: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("schema_version: \"1\"\nwechat:\n  default_account: personal\n  accounts:\n    personal:\n      app_id: wx1234567890abcdef\n      app_secret: test-secret\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	coverBytes, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	coverPath := filepath.Join(projectRoot, "cover.png")
	if err := os.WriteFile(coverPath, coverBytes, 0o644); err != nil {
		t.Fatalf("write cover: %v", err)
	}
	service := publisher.NewService(confirmedDraftPort{})
	plan, err := service.Plan(ctx, publisher.DraftPlanRequest{WorkspaceRoot: projectRoot, BuildPath: built.BuildPath, ConfigPath: configPath, CoverPath: coverPath})
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	plan.ConfirmationToken = "-leading-token"
	encodedPlan, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatalf("encode leading-dash plan: %v", err)
	}
	if err := os.WriteFile(plan.PlanPath, append(encodedPlan, '\n'), 0o600); err != nil {
		t.Fatalf("persist leading-dash plan: %v", err)
	}

	var stdout, stderr bytes.Buffer
	exitCode := cli.NewRunnerWithPublisher(&stdout, &stderr, service).Run([]string{"draft", "--plan", plan.PlanPath, "--confirm", plan.ConfirmationToken, "--config", configPath, "--json"})
	if exitCode != 0 {
		t.Fatalf("confirm exit = %d; stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
	var response struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Data    struct {
			Outcome string `json:"outcome"`
			MediaID string `json:"media_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v; %s", err, stdout.String())
	}
	if !response.Success || response.Code != "DRAFT_SUBMITTED" || response.Data.Outcome != "confirmed" || response.Data.MediaID != "draft-media-id" {
		t.Errorf("confirm response = %+v", response)
	}
}
