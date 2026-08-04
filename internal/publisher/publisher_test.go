package publisher_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wechatloom/wechatloom/internal/builder"
	"github.com/wechatloom/wechatloom/internal/publisher"
	"github.com/wechatloom/wechatloom/internal/workspace"
)

type inertWeChatPort struct{}

func buildPreviewed(ctx context.Context, request builder.BuildRequest) (builder.BuildResult, error) {
	result, err := builder.New().Build(ctx, request)
	if err != nil {
		return builder.BuildResult{}, err
	}
	if err := builder.MarkPreviewed(result.BuildPath); err != nil {
		return builder.BuildResult{}, err
	}
	return result, nil
}

func (inertWeChatPort) AccessToken(context.Context, publisher.AccountCredentials) (publisher.Token, error) {
	panic("Plan must not request a token")
}

func (inertWeChatPort) UploadContentImage(context.Context, publisher.Token, publisher.MediaFile) (publisher.ContentImage, error) {
	panic("Plan must not upload content images")
}

func (inertWeChatPort) UploadCover(context.Context, publisher.Token, publisher.MediaFile) (publisher.CoverMedia, error) {
	panic("Plan must not upload a cover")
}

func (inertWeChatPort) AddDraft(context.Context, publisher.Token, publisher.WeChatDraft) (publisher.RemoteDraft, error) {
	panic("Plan must not add a draft")
}

func (inertWeChatPort) UpdateDraft(context.Context, publisher.Token, publisher.WeChatDraftUpdate) error {
	panic("Plan must not update a draft")
}

type successfulWeChatPort struct{}

func (successfulWeChatPort) AccessToken(context.Context, publisher.AccountCredentials) (publisher.Token, error) {
	return publisher.Token{Value: "access-token", ExpiresIn: 7200}, nil
}

func (successfulWeChatPort) UploadContentImage(context.Context, publisher.Token, publisher.MediaFile) (publisher.ContentImage, error) {
	return publisher.ContentImage{URL: "https://mmbiz.qpic.cn/content-image"}, nil
}

func (successfulWeChatPort) UploadCover(context.Context, publisher.Token, publisher.MediaFile) (publisher.CoverMedia, error) {
	return publisher.CoverMedia{MediaID: "cover-media-id"}, nil
}

func (successfulWeChatPort) AddDraft(context.Context, publisher.Token, publisher.WeChatDraft) (publisher.RemoteDraft, error) {
	return publisher.RemoteDraft{MediaID: "draft-media-id"}, nil
}

func (successfulWeChatPort) UpdateDraft(context.Context, publisher.Token, publisher.WeChatDraftUpdate) error {
	return nil
}

type recoverableWeChatPort struct {
	contentUploaded bool
	coverUploaded   bool
	addAttempts     int
}

type unknownOutcomePort struct{ successfulWeChatPort }

func (unknownOutcomePort) AddDraft(context.Context, publisher.Token, publisher.WeChatDraft) (publisher.RemoteDraft, error) {
	return publisher.RemoteDraft{}, publisher.NewOutcomeUnknownError(errors.New("connection ended after request write"))
}

type singleTokenPort struct {
	tokenRequests int
}

type concurrentDraftPort struct {
	addCalls atomic.Int32
}

type concurrentMediaPort struct {
	uploads atomic.Int32
	ready   chan struct{}
	once    sync.Once
}

type duplicateMediaPort struct {
	contentCalls   atomic.Int32
	coverCalls     atomic.Int32
	contentStarted chan struct{}
	contentRelease chan struct{}
	coverStarted   chan struct{}
	coverRelease   chan struct{}
	contentOnce    sync.Once
	coverOnce      sync.Once
}

func (*duplicateMediaPort) AccessToken(context.Context, publisher.AccountCredentials) (publisher.Token, error) {
	return publisher.Token{Value: "duplicate-media-token", ExpiresIn: 7200}, nil
}
func (port *duplicateMediaPort) UploadContentImage(_ context.Context, _ publisher.Token, media publisher.MediaFile) (publisher.ContentImage, error) {
	port.contentCalls.Add(1)
	port.contentOnce.Do(func() { close(port.contentStarted) })
	<-port.contentRelease
	return publisher.ContentImage{URL: "https://mmbiz.qpic.cn/" + media.SHA256}, nil
}
func (port *duplicateMediaPort) UploadCover(context.Context, publisher.Token, publisher.MediaFile) (publisher.CoverMedia, error) {
	port.coverCalls.Add(1)
	port.coverOnce.Do(func() { close(port.coverStarted) })
	<-port.coverRelease
	return publisher.CoverMedia{MediaID: "deduplicated-cover"}, nil
}
func (*duplicateMediaPort) AddDraft(_ context.Context, _ publisher.Token, draft publisher.WeChatDraft) (publisher.RemoteDraft, error) {
	return publisher.RemoteDraft{MediaID: "draft-" + draft.Title}, nil
}
func (*duplicateMediaPort) UpdateDraft(context.Context, publisher.Token, publisher.WeChatDraftUpdate) error {
	return nil
}

func (port *concurrentMediaPort) AccessToken(context.Context, publisher.AccountCredentials) (publisher.Token, error) {
	return publisher.Token{Value: "concurrent-media-token", ExpiresIn: 7200}, nil
}
func (port *concurrentMediaPort) UploadContentImage(_ context.Context, _ publisher.Token, media publisher.MediaFile) (publisher.ContentImage, error) {
	if port.uploads.Add(1) == 2 {
		port.once.Do(func() { close(port.ready) })
	}
	<-port.ready
	return publisher.ContentImage{URL: "https://mmbiz.qpic.cn/" + media.SHA256}, nil
}
func (*concurrentMediaPort) UploadCover(context.Context, publisher.Token, publisher.MediaFile) (publisher.CoverMedia, error) {
	return publisher.CoverMedia{MediaID: "shared-cover"}, nil
}
func (*concurrentMediaPort) AddDraft(_ context.Context, _ publisher.Token, draft publisher.WeChatDraft) (publisher.RemoteDraft, error) {
	return publisher.RemoteDraft{MediaID: "draft-" + draft.Title}, nil
}
func (*concurrentMediaPort) UpdateDraft(context.Context, publisher.Token, publisher.WeChatDraftUpdate) error {
	return nil
}

func (*concurrentDraftPort) AccessToken(context.Context, publisher.AccountCredentials) (publisher.Token, error) {
	return publisher.Token{Value: "concurrent-token", ExpiresIn: 7200}, nil
}
func (*concurrentDraftPort) UploadContentImage(context.Context, publisher.Token, publisher.MediaFile) (publisher.ContentImage, error) {
	return publisher.ContentImage{URL: "https://mmbiz.qpic.cn/content"}, nil
}
func (*concurrentDraftPort) UploadCover(context.Context, publisher.Token, publisher.MediaFile) (publisher.CoverMedia, error) {
	return publisher.CoverMedia{MediaID: "cover"}, nil
}
func (port *concurrentDraftPort) AddDraft(context.Context, publisher.Token, publisher.WeChatDraft) (publisher.RemoteDraft, error) {
	port.addCalls.Add(1)
	time.Sleep(100 * time.Millisecond)
	return publisher.RemoteDraft{MediaID: "single-draft"}, nil
}
func (*concurrentDraftPort) UpdateDraft(context.Context, publisher.Token, publisher.WeChatDraftUpdate) error {
	return nil
}

func (port *singleTokenPort) AccessToken(context.Context, publisher.AccountCredentials) (publisher.Token, error) {
	port.tokenRequests++
	if port.tokenRequests > 1 {
		return publisher.Token{}, errors.New("token endpoint called more than once")
	}
	return publisher.Token{Value: "cached-access-token", ExpiresIn: 7200}, nil
}

func (*singleTokenPort) UploadContentImage(context.Context, publisher.Token, publisher.MediaFile) (publisher.ContentImage, error) {
	return publisher.ContentImage{URL: "https://mmbiz.qpic.cn/content"}, nil
}

func (*singleTokenPort) UploadCover(context.Context, publisher.Token, publisher.MediaFile) (publisher.CoverMedia, error) {
	return publisher.CoverMedia{MediaID: "cover"}, nil
}

func (*singleTokenPort) AddDraft(context.Context, publisher.Token, publisher.WeChatDraft) (publisher.RemoteDraft, error) {
	return publisher.RemoteDraft{MediaID: "draft"}, nil
}

func (*singleTokenPort) UpdateDraft(context.Context, publisher.Token, publisher.WeChatDraftUpdate) error {
	return nil
}

func (port *recoverableWeChatPort) AccessToken(context.Context, publisher.AccountCredentials) (publisher.Token, error) {
	return publisher.Token{Value: "access-token", ExpiresIn: 7200}, nil
}

func (port *recoverableWeChatPort) UploadContentImage(context.Context, publisher.Token, publisher.MediaFile) (publisher.ContentImage, error) {
	if port.contentUploaded {
		return publisher.ContentImage{}, errors.New("content image was uploaded twice")
	}
	port.contentUploaded = true
	return publisher.ContentImage{URL: "https://mmbiz.qpic.cn/reusable-content"}, nil
}

func (port *recoverableWeChatPort) UploadCover(context.Context, publisher.Token, publisher.MediaFile) (publisher.CoverMedia, error) {
	if port.coverUploaded {
		return publisher.CoverMedia{}, errors.New("cover was uploaded twice")
	}
	port.coverUploaded = true
	return publisher.CoverMedia{MediaID: "reusable-cover"}, nil
}

func (port *recoverableWeChatPort) AddDraft(context.Context, publisher.Token, publisher.WeChatDraft) (publisher.RemoteDraft, error) {
	port.addAttempts++
	if port.addAttempts == 1 {
		return publisher.RemoteDraft{}, errors.New("known draft rejection")
	}
	return publisher.RemoteDraft{MediaID: "recovered-draft"}, nil
}

func (port *recoverableWeChatPort) UpdateDraft(context.Context, publisher.Token, publisher.WeChatDraftUpdate) error {
	return errors.New("unexpected update")
}

func TestPlanCreatesRecoverableDryRunWithoutRemoteCalls(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	if _, err := workspace.NewLocal().Init(ctx, projectRoot); err != nil {
		t.Fatalf("init workspace: %v", err)
	}
	sourcePath := filepath.Join(projectRoot, "article.md")
	if err := os.WriteFile(sourcePath, []byte("---\ntitle: Safe draft\nauthor: Loom\n---\n\n# Safe draft\n\nHello.\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	built, err := buildPreviewed(ctx, builder.BuildRequest{WorkspaceRoot: projectRoot, SourcePath: sourcePath})
	if err != nil {
		t.Fatalf("build article: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(`schema_version: "1"
wechat:
  default_account: personal
  accounts:
    personal:
      app_id: wx1234567890abcdef
      app_secret: test-secret
`), 0o600); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	service := publisher.NewService(inertWeChatPort{})
	plan, err := service.Plan(ctx, publisher.DraftPlanRequest{
		WorkspaceRoot: projectRoot,
		BuildPath:     built.BuildPath,
		ConfigPath:    configPath,
	})
	if err != nil {
		t.Fatalf("plan draft: %v", err)
	}
	if plan.Operation != "add" || plan.Account != "personal" || plan.MaskedAppID != "wx12…cdef" {
		t.Errorf("plan target = %+v, want add to masked personal account", plan)
	}
	if plan.ContentHash != built.ContentHash || plan.ConfirmationToken == "" || plan.ExpiresAt.IsZero() {
		t.Errorf("plan integrity fields = %+v, want build hash and expiring confirmation", plan)
	}
	if plan.PlanPath == "" {
		t.Fatal("plan path is empty")
	}
	if _, err := os.Stat(plan.PlanPath); err != nil {
		t.Fatalf("recoverable plan was not persisted: %v", err)
	}
}

func TestPlanRejectsCredentialConfigurationInsideTheProject(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	if _, err := workspace.NewLocal().Init(ctx, projectRoot); err != nil {
		t.Fatalf("init workspace: %v", err)
	}
	sourcePath := filepath.Join(projectRoot, "article.md")
	if err := os.WriteFile(sourcePath, []byte("# Secret scope\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	built, err := buildPreviewed(ctx, builder.BuildRequest{WorkspaceRoot: projectRoot, SourcePath: sourcePath})
	if err != nil {
		t.Fatalf("build article: %v", err)
	}
	configPath := filepath.Join(projectRoot, "credentials.yaml")
	if err := os.WriteFile(configPath, []byte("schema_version: \"1\"\nwechat:\n  default_account: personal\n  accounts:\n    personal:\n      app_id: wx1234567890abcdef\n      app_secret: test-secret\n"), 0o600); err != nil {
		t.Fatalf("write project credentials: %v", err)
	}
	_, err = publisher.NewService(inertWeChatPort{}).Plan(ctx, publisher.DraftPlanRequest{
		WorkspaceRoot: projectRoot, BuildPath: built.BuildPath, ConfigPath: configPath,
	})
	if err == nil || !strings.Contains(err.Error(), "USER_CONFIG_SCOPE") {
		t.Fatalf("project credential error = %v, want USER_CONFIG_SCOPE", err)
	}
}

func TestPlanRejectsOversizedCoverBeforeAnyRemoteWork(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	if _, err := workspace.NewLocal().Init(ctx, projectRoot); err != nil {
		t.Fatalf("init workspace: %v", err)
	}
	sourcePath := filepath.Join(projectRoot, "article.md")
	if err := os.WriteFile(sourcePath, []byte("# Cover size\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	built, err := buildPreviewed(ctx, builder.BuildRequest{WorkspaceRoot: projectRoot, SourcePath: sourcePath})
	if err != nil {
		t.Fatalf("build article: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("schema_version: \"1\"\nwechat:\n  default_account: personal\n  accounts:\n    personal:\n      app_id: wx1234567890abcdef\n      app_secret: test-secret\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	oversized := make([]byte, (10<<20)+1)
	copy(oversized, []byte("\x89PNG\r\n\x1a\n"))
	coverPath := filepath.Join(projectRoot, "large.png")
	if err := os.WriteFile(coverPath, oversized, 0o644); err != nil {
		t.Fatalf("write oversized cover: %v", err)
	}
	_, err = publisher.NewService(inertWeChatPort{}).Plan(ctx, publisher.DraftPlanRequest{
		WorkspaceRoot: projectRoot, BuildPath: built.BuildPath, ConfigPath: configPath, CoverPath: coverPath,
	})
	if err == nil || !strings.Contains(err.Error(), "MEDIA_SIZE") {
		t.Fatalf("oversized cover error = %v, want MEDIA_SIZE", err)
	}
}

func TestConfirmedSubmitPersistsSuccessBeforeFuturePlansChooseUpdate(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	if _, err := workspace.NewLocal().Init(ctx, projectRoot); err != nil {
		t.Fatalf("init workspace: %v", err)
	}
	sourcePath := filepath.Join(projectRoot, "article.md")
	if err := os.WriteFile(sourcePath, []byte("---\ntitle: Safe draft\nauthor: Loom\n---\n\n# Safe draft\n\nHello.\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	built, err := buildPreviewed(ctx, builder.BuildRequest{WorkspaceRoot: projectRoot, SourcePath: sourcePath})
	if err != nil {
		t.Fatalf("build article: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(`schema_version: "1"
wechat:
  default_account: personal
  accounts:
    personal:
      app_id: wx1234567890abcdef
      app_secret: test-secret
`), 0o600); err != nil {
		t.Fatalf("write user config: %v", err)
	}
	coverPath := filepath.Join(projectRoot, "cover.png")
	cover, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode cover fixture: %v", err)
	}
	if err := os.WriteFile(coverPath, cover, 0o644); err != nil {
		t.Fatalf("write cover: %v", err)
	}

	service := publisher.NewService(successfulWeChatPort{})
	plan, err := service.Plan(ctx, publisher.DraftPlanRequest{
		WorkspaceRoot: projectRoot,
		BuildPath:     built.BuildPath,
		ConfigPath:    configPath,
		CoverPath:     coverPath,
	})
	if err != nil {
		t.Fatalf("plan draft: %v", err)
	}
	result, err := service.Submit(ctx, publisher.ConfirmedDraftRequest{
		PlanPath:          plan.PlanPath,
		ConfirmationToken: plan.ConfirmationToken,
		ConfigPath:        configPath,
	})
	if err != nil {
		t.Fatalf("submit draft: %v", err)
	}
	if result.Outcome != "confirmed" || result.Operation != "add" || result.MediaID != "draft-media-id" {
		t.Errorf("submit result = %+v, want confirmed add", result)
	}
	if _, err := os.Stat(result.StatePath); err != nil {
		t.Fatalf("confirmed result state was not persisted: %v", err)
	}

	next, err := service.Plan(ctx, publisher.DraftPlanRequest{
		WorkspaceRoot: projectRoot,
		BuildPath:     built.BuildPath,
		ConfigPath:    configPath,
		CoverPath:     coverPath,
	})
	if err != nil {
		t.Fatalf("plan update: %v", err)
	}
	if next.Operation != "update" {
		t.Errorf("next operation = %q, want update", next.Operation)
	}
	if err := os.WriteFile(sourcePath, []byte("---\ntitle: Safe draft revised\nauthor: Loom\n---\n\n# Safe draft revised\n\nChanged.\n"), 0o644); err != nil {
		t.Fatalf("revise source: %v", err)
	}
	rebuilt, err := buildPreviewed(ctx, builder.BuildRequest{WorkspaceRoot: projectRoot, SourcePath: sourcePath})
	if err != nil {
		t.Fatalf("rebuild revised article: %v", err)
	}
	revised, err := service.Plan(ctx, publisher.DraftPlanRequest{
		WorkspaceRoot: projectRoot,
		BuildPath:     rebuilt.BuildPath,
		ConfigPath:    configPath,
		CoverPath:     coverPath,
	})
	if err != nil {
		t.Fatalf("plan revised article: %v", err)
	}
	if revised.Operation != "update" {
		t.Errorf("revised source operation = %q, want update of the associated draft", revised.Operation)
	}
}

func TestFailedDraftSubmissionReusesAlreadyUploadedMediaOnAConfirmedRetry(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	if _, err := workspace.NewLocal().Init(ctx, projectRoot); err != nil {
		t.Fatalf("init workspace: %v", err)
	}
	imageBytes, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatalf("decode image fixture: %v", err)
	}
	imagePath := filepath.Join(projectRoot, "cover.png")
	if err := os.WriteFile(imagePath, imageBytes, 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	sourcePath := filepath.Join(projectRoot, "article.md")
	if err := os.WriteFile(sourcePath, []byte("---\ntitle: Recoverable draft\n---\n\n# Recoverable draft\n\n![figure](cover.png)\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	built, err := buildPreviewed(ctx, builder.BuildRequest{WorkspaceRoot: projectRoot, SourcePath: sourcePath})
	if err != nil {
		t.Fatalf("build article: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("schema_version: \"1\"\nwechat:\n  default_account: personal\n  accounts:\n    personal:\n      app_id: wx1234567890abcdef\n      app_secret: test-secret\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	port := &recoverableWeChatPort{}
	service := publisher.NewService(port)

	first, err := service.Plan(ctx, publisher.DraftPlanRequest{WorkspaceRoot: projectRoot, BuildPath: built.BuildPath, ConfigPath: configPath, CoverPath: imagePath})
	if err != nil {
		t.Fatalf("first plan: %v", err)
	}
	if _, err := service.Submit(ctx, publisher.ConfirmedDraftRequest{PlanPath: first.PlanPath, ConfirmationToken: first.ConfirmationToken, ConfigPath: configPath}); err == nil {
		t.Fatal("first submission succeeded, want simulated draft rejection")
	}
	second, err := service.Plan(ctx, publisher.DraftPlanRequest{WorkspaceRoot: projectRoot, BuildPath: built.BuildPath, ConfigPath: configPath, CoverPath: imagePath})
	if err != nil {
		t.Fatalf("retry plan: %v", err)
	}
	result, err := service.Submit(ctx, publisher.ConfirmedDraftRequest{PlanPath: second.PlanPath, ConfirmationToken: second.ConfirmationToken, ConfigPath: configPath})
	if err != nil {
		t.Fatalf("confirmed retry did not reuse uploaded media: %v", err)
	}
	if result.MediaID != "recovered-draft" {
		t.Errorf("retry media ID = %q, want recovered-draft", result.MediaID)
	}
}

func TestUnknownWriteOutcomeIsPersistedAndBlocksAutomaticRetry(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	if _, err := workspace.NewLocal().Init(ctx, projectRoot); err != nil {
		t.Fatalf("init workspace: %v", err)
	}
	coverBytes, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	coverPath := filepath.Join(projectRoot, "cover.png")
	if err := os.WriteFile(coverPath, coverBytes, 0o644); err != nil {
		t.Fatalf("write cover: %v", err)
	}
	sourcePath := filepath.Join(projectRoot, "article.md")
	if err := os.WriteFile(sourcePath, []byte("---\ntitle: Unknown outcome\n---\n\n# Unknown outcome\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	built, err := buildPreviewed(ctx, builder.BuildRequest{WorkspaceRoot: projectRoot, SourcePath: sourcePath})
	if err != nil {
		t.Fatalf("build article: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("schema_version: \"1\"\nwechat:\n  default_account: personal\n  accounts:\n    personal:\n      app_id: wx1234567890abcdef\n      app_secret: test-secret\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	service := publisher.NewService(unknownOutcomePort{})
	plan, err := service.Plan(ctx, publisher.DraftPlanRequest{WorkspaceRoot: projectRoot, BuildPath: built.BuildPath, ConfigPath: configPath, CoverPath: coverPath})
	if err != nil {
		t.Fatalf("plan draft: %v", err)
	}
	result, err := service.Submit(ctx, publisher.ConfirmedDraftRequest{PlanPath: plan.PlanPath, ConfirmationToken: plan.ConfirmationToken, ConfigPath: configPath})
	if err == nil {
		t.Fatal("submit returned nil error for an unknown remote outcome")
	}
	if result.Outcome != "outcome_unknown" || result.StatePath == "" {
		t.Errorf("unknown result = %+v, want persisted outcome_unknown", result)
	}
	if _, err := service.Plan(ctx, publisher.DraftPlanRequest{WorkspaceRoot: projectRoot, BuildPath: built.BuildPath, ConfigPath: configPath, CoverPath: coverPath}); err == nil || !strings.Contains(err.Error(), "DRAFT_RECONCILIATION_REQUIRED") {
		t.Fatalf("new plan after unknown outcome error = %v, want reconciliation gate", err)
	}
	status, err := service.Reconcile(ctx, publisher.ReconcileRequest{PlanPath: plan.PlanPath, Result: "absent"})
	if err != nil {
		t.Fatalf("reconcile manually confirmed absence: %v", err)
	}
	if status.Outcome != "failed_known" {
		t.Errorf("reconciled outcome = %q, want failed_known", status.Outcome)
	}
	if _, err := service.Plan(ctx, publisher.DraftPlanRequest{WorkspaceRoot: projectRoot, BuildPath: built.BuildPath, ConfigPath: configPath, CoverPath: coverPath}); err != nil {
		t.Fatalf("plan remained blocked after reconciliation: %v", err)
	}
}

func TestUnexpiredTokenCacheIsReusedAcrossServiceInstances(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	if _, err := workspace.NewLocal().Init(ctx, projectRoot); err != nil {
		t.Fatalf("init workspace: %v", err)
	}
	coverBytes, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	coverPath := filepath.Join(projectRoot, "cover.png")
	if err := os.WriteFile(coverPath, coverBytes, 0o644); err != nil {
		t.Fatalf("write cover: %v", err)
	}
	sourcePath := filepath.Join(projectRoot, "article.md")
	if err := os.WriteFile(sourcePath, []byte("---\ntitle: Cached token\n---\n\n# Cached token\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	built, err := buildPreviewed(ctx, builder.BuildRequest{WorkspaceRoot: projectRoot, SourcePath: sourcePath})
	if err != nil {
		t.Fatalf("build article: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("schema_version: \"1\"\nwechat:\n  default_account: personal\n  accounts:\n    personal:\n      app_id: wx1234567890abcdef\n      app_secret: test-secret\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	port := &singleTokenPort{}
	firstService := publisher.NewService(port)
	first, err := firstService.Plan(ctx, publisher.DraftPlanRequest{WorkspaceRoot: projectRoot, BuildPath: built.BuildPath, ConfigPath: configPath, CoverPath: coverPath})
	if err != nil {
		t.Fatalf("first plan: %v", err)
	}
	if _, err := firstService.Submit(ctx, publisher.ConfirmedDraftRequest{PlanPath: first.PlanPath, ConfirmationToken: first.ConfirmationToken, ConfigPath: configPath}); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	secondService := publisher.NewService(port)
	second, err := secondService.Plan(ctx, publisher.DraftPlanRequest{WorkspaceRoot: projectRoot, BuildPath: built.BuildPath, ConfigPath: configPath, CoverPath: coverPath})
	if err != nil {
		t.Fatalf("update plan: %v", err)
	}
	if _, err := secondService.Submit(ctx, publisher.ConfirmedDraftRequest{PlanPath: second.PlanPath, ConfirmationToken: second.ConfirmationToken, ConfigPath: configPath}); err != nil {
		t.Fatalf("update did not reuse token cache: %v", err)
	}
}

func TestConcurrentConfirmationsCreateOnlyOneRemoteDraft(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	if _, err := workspace.NewLocal().Init(ctx, projectRoot); err != nil {
		t.Fatalf("init workspace: %v", err)
	}
	sourcePath := filepath.Join(projectRoot, "article.md")
	if err := os.WriteFile(sourcePath, []byte("---\ntitle: Concurrent draft\n---\n\n# Concurrent draft\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	built, err := buildPreviewed(ctx, builder.BuildRequest{WorkspaceRoot: projectRoot, SourcePath: sourcePath})
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
	port := &concurrentDraftPort{}
	service := publisher.NewService(port)
	plan, err := service.Plan(ctx, publisher.DraftPlanRequest{WorkspaceRoot: projectRoot, BuildPath: built.BuildPath, ConfigPath: configPath, CoverPath: coverPath})
	if err != nil {
		t.Fatalf("plan draft: %v", err)
	}
	errorsOut := make(chan error, 2)
	for range 2 {
		go func() {
			_, submitErr := service.Submit(ctx, publisher.ConfirmedDraftRequest{PlanPath: plan.PlanPath, ConfirmationToken: plan.ConfirmationToken, ConfigPath: configPath})
			errorsOut <- submitErr
		}()
	}
	successes := 0
	for range 2 {
		if <-errorsOut == nil {
			successes++
		}
	}
	if successes != 1 || port.addCalls.Load() != 1 {
		t.Fatalf("concurrent results: successes=%d add_calls=%d, want exactly one remote add", successes, port.addCalls.Load())
	}
}

func TestChangingAnAliasToAnotherAppIDDoesNotReuseTheFormerDraft(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	if _, err := workspace.NewLocal().Init(ctx, projectRoot); err != nil {
		t.Fatalf("init workspace: %v", err)
	}
	sourcePath := filepath.Join(projectRoot, "article.md")
	if err := os.WriteFile(sourcePath, []byte("---\ntitle: Account identity\n---\n\n# Account identity\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	built, err := buildPreviewed(ctx, builder.BuildRequest{WorkspaceRoot: projectRoot, SourcePath: sourcePath})
	if err != nil {
		t.Fatalf("build article: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig := func(appID, secret string) {
		t.Helper()
		content := "schema_version: \"1\"\nwechat:\n  default_account: personal\n  accounts:\n    personal:\n      app_id: " + appID + "\n      app_secret: " + secret + "\n"
		if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	coverBytes, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	coverPath := filepath.Join(projectRoot, "cover.png")
	if err := os.WriteFile(coverPath, coverBytes, 0o644); err != nil {
		t.Fatalf("write cover: %v", err)
	}
	service := publisher.NewService(successfulWeChatPort{})
	writeConfig("wx1111111111111111", "first-secret")
	first, err := service.Plan(ctx, publisher.DraftPlanRequest{WorkspaceRoot: projectRoot, BuildPath: built.BuildPath, ConfigPath: configPath, CoverPath: coverPath})
	if err != nil {
		t.Fatalf("plan first account: %v", err)
	}
	if _, err := service.Submit(ctx, publisher.ConfirmedDraftRequest{PlanPath: first.PlanPath, ConfirmationToken: first.ConfirmationToken, ConfigPath: configPath}); err != nil {
		t.Fatalf("submit first account: %v", err)
	}

	writeConfig("wx2222222222222222", "second-secret")
	second, err := service.Plan(ctx, publisher.DraftPlanRequest{WorkspaceRoot: projectRoot, BuildPath: built.BuildPath, ConfigPath: configPath, CoverPath: coverPath})
	if err != nil {
		t.Fatalf("plan rebound alias: %v", err)
	}
	if second.Operation != "add" {
		t.Fatalf("rebound alias operation = %q, want add for a distinct AppID", second.Operation)
	}
}

func TestConcurrentArticlesMergeTheirAccountMediaCache(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	if _, err := workspace.NewLocal().Init(ctx, projectRoot); err != nil {
		t.Fatalf("init workspace: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("schema_version: \"1\"\nwechat:\n  default_account: personal\n  accounts:\n    personal:\n      app_id: wx1234567890abcdef\n      app_secret: test-secret\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	makeImage := func(path string, shade uint8) {
		t.Helper()
		picture := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		picture.Set(0, 0, color.NRGBA{R: shade, G: 30, B: 40, A: 255})
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, picture); err != nil {
			t.Fatalf("encode image: %v", err)
		}
		if err := os.WriteFile(path, encoded.Bytes(), 0o644); err != nil {
			t.Fatalf("write image: %v", err)
		}
	}
	coverPath := filepath.Join(projectRoot, "cover.png")
	makeImage(coverPath, 10)
	builds := make([]builder.BuildResult, 0, 2)
	for index, shade := range []uint8{80, 160} {
		imageName := fmt.Sprintf("image-%d.png", index)
		makeImage(filepath.Join(projectRoot, imageName), shade)
		sourcePath := filepath.Join(projectRoot, fmt.Sprintf("article-%d.md", index))
		content := fmt.Sprintf("---\ntitle: Concurrent %d\n---\n\n# Concurrent %d\n\n![image](./%s)\n", index, index, imageName)
		if err := os.WriteFile(sourcePath, []byte(content), 0o644); err != nil {
			t.Fatalf("write source: %v", err)
		}
		built, err := buildPreviewed(ctx, builder.BuildRequest{WorkspaceRoot: projectRoot, SourcePath: sourcePath})
		if err != nil {
			t.Fatalf("build article %d: %v", index, err)
		}
		builds = append(builds, built)
	}
	port := &concurrentMediaPort{ready: make(chan struct{})}
	service := publisher.NewService(port)
	plans := make([]publisher.DraftPlan, 0, 2)
	for _, built := range builds {
		plan, err := service.Plan(ctx, publisher.DraftPlanRequest{WorkspaceRoot: projectRoot, BuildPath: built.BuildPath, ConfigPath: configPath, CoverPath: coverPath})
		if err != nil {
			t.Fatalf("plan article: %v", err)
		}
		plans = append(plans, plan)
	}
	errorsOut := make(chan error, 2)
	for _, plan := range plans {
		plan := plan
		go func() {
			_, submitErr := service.Submit(ctx, publisher.ConfirmedDraftRequest{PlanPath: plan.PlanPath, ConfirmationToken: plan.ConfirmationToken, ConfigPath: configPath})
			errorsOut <- submitErr
		}()
	}
	for range 2 {
		if err := <-errorsOut; err != nil {
			t.Fatalf("submit concurrent article: %v", err)
		}
	}
	resolved, err := workspace.NewLocal().Resolve(ctx, projectRoot)
	if err != nil {
		t.Fatalf("resolve workspace: %v", err)
	}
	cachePaths, err := filepath.Glob(filepath.Join(resolved.StatePath, "media", "*.json"))
	if err != nil || len(cachePaths) != 1 {
		t.Fatalf("media cache paths = %v, err=%v", cachePaths, err)
	}
	var cache struct {
		ContentImages map[string]string `json:"content_images"`
	}
	content, err := os.ReadFile(cachePaths[0])
	if err != nil || json.Unmarshal(content, &cache) != nil {
		t.Fatalf("read media cache: %v", err)
	}
	if len(cache.ContentImages) != 2 {
		t.Fatalf("cached content images = %d, want both concurrent uploads; cache=%s", len(cache.ContentImages), content)
	}
}

func TestConcurrentArticlesUploadEachAccountMediaHashOnlyOnce(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	if _, err := workspace.NewLocal().Init(ctx, projectRoot); err != nil {
		t.Fatalf("init workspace: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("schema_version: \"1\"\nwechat:\n  default_account: personal\n  accounts:\n    personal:\n      app_id: wx1234567890abcdef\n      app_secret: test-secret\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	imageBytes, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	imagePath := filepath.Join(projectRoot, "shared.png")
	if err := os.WriteFile(imagePath, imageBytes, 0o644); err != nil {
		t.Fatalf("write shared image: %v", err)
	}
	builds := make([]builder.BuildResult, 0, 2)
	for index := range 2 {
		sourcePath := filepath.Join(projectRoot, fmt.Sprintf("shared-%d.md", index))
		content := fmt.Sprintf("---\ntitle: Shared %d\n---\n\n# Shared %d\n\n![shared](./shared.png)\n", index, index)
		if err := os.WriteFile(sourcePath, []byte(content), 0o644); err != nil {
			t.Fatalf("write source: %v", err)
		}
		built, err := buildPreviewed(ctx, builder.BuildRequest{WorkspaceRoot: projectRoot, SourcePath: sourcePath})
		if err != nil {
			t.Fatalf("build article: %v", err)
		}
		builds = append(builds, built)
	}
	port := &duplicateMediaPort{
		contentStarted: make(chan struct{}), contentRelease: make(chan struct{}),
		coverStarted: make(chan struct{}), coverRelease: make(chan struct{}),
	}
	service := publisher.NewService(port)
	plans := make([]publisher.DraftPlan, 0, 2)
	for _, built := range builds {
		plan, err := service.Plan(ctx, publisher.DraftPlanRequest{WorkspaceRoot: projectRoot, BuildPath: built.BuildPath, ConfigPath: configPath, CoverPath: imagePath})
		if err != nil {
			t.Fatalf("plan article: %v", err)
		}
		plans = append(plans, plan)
	}
	errorsOut := make(chan error, 2)
	for _, plan := range plans {
		plan := plan
		go func() {
			_, submitErr := service.Submit(ctx, publisher.ConfirmedDraftRequest{PlanPath: plan.PlanPath, ConfirmationToken: plan.ConfirmationToken, ConfigPath: configPath})
			errorsOut <- submitErr
		}()
	}
	<-port.contentStarted
	time.Sleep(100 * time.Millisecond)
	close(port.contentRelease)
	<-port.coverStarted
	time.Sleep(100 * time.Millisecond)
	close(port.coverRelease)
	for range 2 {
		if err := <-errorsOut; err != nil {
			t.Fatalf("submit shared-media article: %v", err)
		}
	}
	if port.contentCalls.Load() != 1 || port.coverCalls.Load() != 1 {
		t.Fatalf("remote media uploads: content=%d cover=%d, want one per account hash", port.contentCalls.Load(), port.coverCalls.Load())
	}
}
