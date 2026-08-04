package publisher

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wechatloom/wechatloom/internal/builder"
	"github.com/wechatloom/wechatloom/internal/workspace"
)

const confirmationLifetime = 15 * time.Minute

const (
	maximumMediaBytes  = 10 << 20
	maximumMediaPixels = 40_000_000
)

type MediaFile struct {
	Path      string
	MediaType string
	Content   []byte
	SHA256    string
}

type ContentImage struct {
	URL string
}

type CoverMedia struct {
	MediaID string
}

type WeChatDraft struct {
	Title           string `json:"title"`
	Author          string `json:"author,omitempty"`
	Digest          string `json:"digest,omitempty"`
	Content         string `json:"content"`
	ThumbMediaID    string `json:"thumb_media_id"`
	ContentSource   string `json:"content_source_url,omitempty"`
	NeedOpenComment int    `json:"need_open_comment,omitempty"`
}

type WeChatDraftUpdate struct {
	MediaID string
	Draft   WeChatDraft
}

type RemoteDraft struct {
	MediaID string
}

type WeChatPort interface {
	AccessToken(context.Context, AccountCredentials) (Token, error)
	UploadContentImage(context.Context, Token, MediaFile) (ContentImage, error)
	UploadCover(context.Context, Token, MediaFile) (CoverMedia, error)
	AddDraft(context.Context, Token, WeChatDraft) (RemoteDraft, error)
	UpdateDraft(context.Context, Token, WeChatDraftUpdate) error
}

type DraftPlanRequest struct {
	WorkspaceRoot string
	BuildPath     string
	ConfigPath    string
	Account       string
	CoverPath     string
	NewDraft      bool
}

type DraftPlan struct {
	SchemaVersion     string    `json:"schema_version"`
	ID                string    `json:"id"`
	PlanPath          string    `json:"plan_path"`
	WorkspaceRoot     string    `json:"workspace_root"`
	BuildPath         string    `json:"build_path"`
	BuildID           string    `json:"build_id"`
	ContentHash       string    `json:"content_hash"`
	SourcePath        string    `json:"source_path"`
	SourceHash        string    `json:"source_hash"`
	Account           string    `json:"account"`
	MaskedAppID       string    `json:"masked_app_id"`
	AccountBinding    string    `json:"account_binding"`
	AccountStateKey   string    `json:"account_state_key"`
	Operation         string    `json:"operation"`
	Title             string    `json:"title"`
	ContentImages     []string  `json:"content_images"`
	ExpectedEffects   []string  `json:"expected_side_effects"`
	Warnings          []string  `json:"warnings"`
	CoverPath         string    `json:"cover_path,omitempty"`
	CoverSHA256       string    `json:"cover_sha256,omitempty"`
	NewDraft          bool      `json:"new_draft,omitempty"`
	ConfirmationToken string    `json:"confirmation_token"`
	CreatedAt         time.Time `json:"created_at"`
	ExpiresAt         time.Time `json:"expires_at"`
}

type ConfirmedDraftRequest struct {
	PlanPath          string
	ConfirmationToken string
	ConfigPath        string
}

type DraftResult struct {
	Outcome     string `json:"outcome"`
	Operation   string `json:"operation"`
	Account     string `json:"account"`
	MaskedAppID string `json:"masked_app_id"`
	MediaID     string `json:"media_id,omitempty"`
	ContentHash string `json:"content_hash"`
	StatePath   string `json:"state_path"`
}

type OutcomeUnknownError struct {
	cause error
}

func NewOutcomeUnknownError(cause error) error {
	return &OutcomeUnknownError{cause: cause}
}

func (err *OutcomeUnknownError) Error() string {
	return "WECHAT_OUTCOME_UNKNOWN: the draft write may have succeeded; inspect the WeChat draft list before reconciling"
}

func (err *OutcomeUnknownError) Unwrap() error { return err.cause }

type draftState struct {
	SchemaVersion   string    `json:"schema_version"`
	Account         string    `json:"account"`
	AccountStateKey string    `json:"account_state_key"`
	SourcePath      string    `json:"source_path"`
	SourceHash      string    `json:"source_hash"`
	ContentHash     string    `json:"content_hash"`
	MediaID         string    `json:"media_id"`
	LastPlanID      string    `json:"last_plan_id"`
	Outcome         string    `json:"outcome,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type mediaCacheState struct {
	SchemaVersion   string            `json:"schema_version"`
	Account         string            `json:"account"`
	AccountStateKey string            `json:"account_state_key"`
	ContentImages   map[string]string `json:"content_images"`
	Covers          map[string]string `json:"covers"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

type tokenCacheRecord struct {
	SchemaVersion string    `json:"schema_version"`
	AccountKey    string    `json:"account_key"`
	Value         string    `json:"access_token"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type buildManifest struct {
	SchemaVersion string `json:"schema_version"`
	BuildID       string `json:"build_id"`
	ContentHash   string `json:"content_hash"`
	Source        struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"source"`
	Artifacts []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"artifacts"`
}

func (service *Service) Plan(ctx context.Context, request DraftPlanRequest) (DraftPlan, error) {
	if err := ctx.Err(); err != nil {
		return DraftPlan{}, err
	}
	resolved, err := workspace.NewLocal().Resolve(ctx, request.WorkspaceRoot)
	if err != nil {
		return DraftPlan{}, err
	}
	buildPath, err := filepath.Abs(request.BuildPath)
	if err != nil {
		return DraftPlan{}, fmt.Errorf("resolve build path: %w", err)
	}
	if !pathWithin(resolved.BuildsPath, buildPath) || filepath.Dir(buildPath) != resolved.BuildsPath {
		return DraftPlan{}, errors.New("DRAFT_BUILD_INVALID: build path must name a committed workspace build")
	}
	manifest, err := readAndValidateBuild(buildPath)
	if err != nil {
		return DraftPlan{}, err
	}
	inspection, err := builder.New().Inspect(ctx, builder.InspectRequest{SourcePath: manifest.Source.Path})
	if err != nil {
		return DraftPlan{}, fmt.Errorf("inspect draft source: %w", err)
	}
	if len(inspection.Errors) != 0 {
		return DraftPlan{}, fmt.Errorf("DRAFT_SOURCE_INVALID: %s", inspection.Errors[0].Message)
	}
	configPath, err := resolveUserConfigPath(request.ConfigPath)
	if err != nil {
		return DraftPlan{}, err
	}
	if err := requireUserConfigOutsideProject(configPath, resolved.Root); err != nil {
		return DraftPlan{}, err
	}
	config, err := loadUserConfig(configPath)
	if err != nil {
		return DraftPlan{}, err
	}
	account, credentials, err := selectAccount(config, request.Account)
	if err != nil {
		return DraftPlan{}, err
	}
	accountStateKey := accountStateBinding(account, credentials)
	existingState, err := loadDraftState(draftStatePath(resolved.StatePath, accountStateKey, manifest.Source.Path))
	if err != nil {
		return DraftPlan{}, err
	}
	if existingState.AccountStateKey != "" && existingState.AccountStateKey != accountStateKey {
		return DraftPlan{}, errors.New("DRAFT_STATE_INVALID: account identity does not match")
	}
	if existingState.Outcome == "outcome_unknown" || existingState.Outcome == "submitting" {
		return DraftPlan{}, errors.New("DRAFT_RECONCILIATION_REQUIRED: inspect the WeChat draft list and reconcile local state before retrying")
	}
	planID, err := randomToken(16)
	if err != nil {
		return DraftPlan{}, fmt.Errorf("create plan ID: %w", err)
	}
	confirmation, err := randomToken(32)
	if err != nil {
		return DraftPlan{}, fmt.Errorf("create confirmation token: %w", err)
	}
	now := time.Now().UTC()
	planPath := filepath.Join(resolved.StatePath, "plans", planID+".json")
	requestedCover := request.CoverPath
	if strings.TrimSpace(requestedCover) == "" && strings.TrimSpace(inspection.Cover) != "" {
		requestedCover = inspection.Cover
		if !filepath.IsAbs(requestedCover) {
			requestedCover = filepath.Join(filepath.Dir(manifest.Source.Path), requestedCover)
		}
	}
	coverPath, coverHash, err := resolveCover(requestedCover)
	if err != nil {
		return DraftPlan{}, err
	}
	plan := DraftPlan{
		SchemaVersion: "1", ID: planID, PlanPath: planPath, WorkspaceRoot: resolved.Root,
		BuildPath: buildPath, BuildID: manifest.BuildID, ContentHash: manifest.ContentHash,
		SourcePath: manifest.Source.Path, SourceHash: manifest.Source.SHA256, Account: account, MaskedAppID: maskAppID(credentials.AppID), AccountBinding: credentialBinding(account, credentials), AccountStateKey: accountStateKey,
		Operation: "add", Title: inspection.Title, CoverPath: coverPath, CoverSHA256: coverHash, ConfirmationToken: confirmation,
		NewDraft: request.NewDraft, CreatedAt: now, ExpiresAt: now.Add(confirmationLifetime),
	}
	if !request.NewDraft && strings.TrimSpace(existingState.MediaID) != "" {
		plan.Operation = "update"
	}
	articleHTML, err := os.ReadFile(filepath.Join(buildPath, "article.html"))
	if err != nil {
		return DraftPlan{}, fmt.Errorf("read planned article HTML: %w", err)
	}
	seenImages := map[string]bool{}
	for _, match := range localImagePattern.FindAllStringSubmatch(string(articleHTML), -1) {
		if !seenImages[match[1]] {
			seenImages[match[1]] = true
			plan.ContentImages = append(plan.ContentImages, match[1])
		}
	}
	plan.ExpectedEffects = []string{"request_or_reuse_access_token"}
	if len(plan.ContentImages) != 0 {
		plan.ExpectedEffects = append(plan.ExpectedEffects, fmt.Sprintf("upload_or_reuse_%d_content_images", len(plan.ContentImages)))
	}
	if plan.CoverPath != "" {
		plan.ExpectedEffects = append(plan.ExpectedEffects, "upload_or_reuse_cover")
	} else {
		plan.Warnings = append(plan.Warnings, "A cover is required before submission")
	}
	plan.ExpectedEffects = append(plan.ExpectedEffects, plan.Operation+"_wechat_draft")
	encoded, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return DraftPlan{}, fmt.Errorf("encode draft plan: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := atomicWritePrivate(planPath, encoded); err != nil {
		return DraftPlan{}, fmt.Errorf("persist draft plan: %w", err)
	}
	return plan, nil
}

func (service *Service) Submit(ctx context.Context, request ConfirmedDraftRequest) (DraftResult, error) {
	if err := ctx.Err(); err != nil {
		return DraftResult{}, err
	}
	planBytes, err := os.ReadFile(request.PlanPath)
	if err != nil {
		return DraftResult{}, fmt.Errorf("DRAFT_PLAN_INVALID: read plan: %w", err)
	}
	var plan DraftPlan
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		return DraftResult{}, fmt.Errorf("DRAFT_PLAN_INVALID: decode plan: %w", err)
	}
	resolved, err := workspace.NewLocal().Resolve(ctx, plan.WorkspaceRoot)
	if err != nil {
		return DraftResult{}, err
	}
	requestedPlanPath, err := filepath.Abs(request.PlanPath)
	if err != nil || requestedPlanPath != plan.PlanPath || !pathWithin(filepath.Join(resolved.StatePath, "plans"), requestedPlanPath) {
		return DraftResult{}, errors.New("DRAFT_PLAN_INVALID: plan is outside the workspace state directory")
	}
	if request.ConfirmationToken == "" || request.ConfirmationToken != plan.ConfirmationToken {
		return DraftResult{}, errors.New("DRAFT_CONFIRMATION_INVALID: confirmation token does not match")
	}
	if time.Now().UTC().After(plan.ExpiresAt) {
		return DraftResult{}, errors.New("DRAFT_CONFIRMATION_EXPIRED: create a new dry-run plan")
	}
	unlock, err := workspace.NewLocal().LockArticle(ctx, plan.WorkspaceRoot, plan.Account+"\x00"+plan.SourcePath)
	if err != nil {
		return DraftResult{}, err
	}
	defer unlock()
	manifest, err := readAndValidateBuild(plan.BuildPath)
	if err != nil {
		return DraftResult{}, err
	}
	if manifest.BuildID != plan.BuildID || manifest.ContentHash != plan.ContentHash || manifest.Source.Path != plan.SourcePath || manifest.Source.SHA256 != plan.SourceHash {
		return DraftResult{}, errors.New("DRAFT_BUILD_CHANGED: build identity no longer matches the confirmed plan")
	}
	inspection, err := builder.New().Inspect(ctx, builder.InspectRequest{SourcePath: manifest.Source.Path})
	if err != nil {
		return DraftResult{}, fmt.Errorf("DRAFT_SOURCE_INVALID: %w", err)
	}
	if inspection.SourceHash != plan.SourceHash {
		return DraftResult{}, errors.New("DRAFT_SOURCE_CHANGED: source no longer matches the confirmed plan")
	}
	configPath, err := resolveUserConfigPath(request.ConfigPath)
	if err != nil {
		return DraftResult{}, err
	}
	if err := requireUserConfigOutsideProject(configPath, resolved.Root); err != nil {
		return DraftResult{}, err
	}
	config, err := loadUserConfig(configPath)
	if err != nil {
		return DraftResult{}, err
	}
	account, credentials, err := selectAccount(config, plan.Account)
	if err != nil {
		return DraftResult{}, err
	}
	if maskAppID(credentials.AppID) != plan.MaskedAppID || subtle.ConstantTimeCompare([]byte(credentialBinding(account, credentials)), []byte(plan.AccountBinding)) != 1 {
		return DraftResult{}, errors.New("DRAFT_ACCOUNT_CHANGED: account credentials no longer match the plan")
	}
	accountStateKey := accountStateBinding(account, credentials)
	if plan.AccountStateKey == "" || subtle.ConstantTimeCompare([]byte(accountStateKey), []byte(plan.AccountStateKey)) != 1 {
		return DraftResult{}, errors.New("DRAFT_ACCOUNT_CHANGED: account identity no longer matches the plan")
	}
	statePath := draftStatePath(resolved.StatePath, accountStateKey, plan.SourcePath)
	state, err := loadDraftState(statePath)
	if err != nil {
		return DraftResult{}, err
	}
	if state.LastPlanID == plan.ID {
		return DraftResult{}, errors.New("DRAFT_PLAN_CONSUMED: this confirmed plan was already submitted")
	}
	if state.AccountStateKey != "" && state.AccountStateKey != accountStateKey {
		return DraftResult{}, errors.New("DRAFT_STATE_INVALID: account identity does not match")
	}
	if plan.Operation == "update" && state.MediaID == "" {
		return DraftResult{}, errors.New("DRAFT_STATE_MISSING: update target is unavailable; create a new dry-run plan")
	}
	if plan.Operation == "add" && state.MediaID != "" && !plan.NewDraft {
		return DraftResult{}, errors.New("DRAFT_DUPLICATE: an existing draft is already associated with this source")
	}
	mediaCachePath := filepath.Join(resolved.StatePath, "media", stateKey(accountStateKey)+".json")
	mediaCache, err := loadMediaCache(mediaCachePath, account, accountStateKey)
	if err != nil {
		return DraftResult{}, err
	}
	token, err := service.accessToken(ctx, credentials, account, configPath)
	if err != nil {
		return DraftResult{}, err
	}
	persistMergedMediaCache := func(contentHash, remoteURL, coverHash, coverMediaID string) error {
		unlockCache, lockErr := workspace.NewLocal().LockArticle(ctx, plan.WorkspaceRoot, "media-cache\x00"+accountStateKey)
		if lockErr != nil {
			return lockErr
		}
		defer unlockCache()
		latest, loadErr := loadMediaCache(mediaCachePath, account, accountStateKey)
		if loadErr != nil {
			return loadErr
		}
		for hash, remoteURL := range mediaCache.ContentImages {
			latest.ContentImages[hash] = remoteURL
		}
		for hash, mediaID := range mediaCache.Covers {
			latest.Covers[hash] = mediaID
		}
		if contentHash != "" {
			latest.ContentImages[contentHash] = remoteURL
		}
		if coverHash != "" {
			latest.Covers[coverHash] = coverMediaID
		}
		latest.UpdatedAt = time.Now().UTC()
		if persistErr := persistMediaCache(mediaCachePath, latest); persistErr != nil {
			return persistErr
		}
		for hash, remoteURL := range latest.ContentImages {
			mediaCache.ContentImages[hash] = remoteURL
		}
		for hash, mediaID := range latest.Covers {
			mediaCache.Covers[hash] = mediaID
		}
		mediaCache.UpdatedAt = latest.UpdatedAt
		return nil
	}
	resolveContentImage := func(media MediaFile) (string, error) {
		unlockMedia, lockErr := workspace.NewLocal().LockArticle(ctx, plan.WorkspaceRoot, "media-object\x00"+accountStateKey+"\x00content\x00"+media.SHA256)
		if lockErr != nil {
			return "", lockErr
		}
		defer unlockMedia()
		latest, loadErr := loadMediaCache(mediaCachePath, account, accountStateKey)
		if loadErr != nil {
			return "", loadErr
		}
		if cached := latest.ContentImages[media.SHA256]; cached != "" {
			mediaCache.ContentImages[media.SHA256] = cached
			return cached, nil
		}
		uploaded, uploadErr := service.port.UploadContentImage(ctx, token, media)
		if uploadErr != nil {
			return "", uploadErr
		}
		remoteURL := strings.TrimSpace(uploaded.URL)
		if remoteURL == "" {
			return "", errors.New("WECHAT_CONTENT_IMAGE_RESPONSE: url is required")
		}
		mediaCache.ContentImages[media.SHA256] = remoteURL
		if persistErr := persistMergedMediaCache(media.SHA256, remoteURL, "", ""); persistErr != nil {
			return "", fmt.Errorf("persist uploaded content image cache: %w", persistErr)
		}
		return remoteURL, nil
	}

	articleBytes, err := os.ReadFile(filepath.Join(plan.BuildPath, "article.html"))
	if err != nil {
		return DraftResult{}, fmt.Errorf("read article HTML: %w", err)
	}
	articleHTML, err := service.materializeContentImages(plan.BuildPath, string(articleBytes), mediaCache.ContentImages, resolveContentImage)
	if err != nil {
		return DraftResult{}, err
	}
	if plan.CoverPath == "" {
		return DraftResult{}, errors.New("DRAFT_COVER_REQUIRED: provide --cover when creating the dry-run plan")
	}
	cover, err := readMediaFile(plan.CoverPath)
	if err != nil {
		return DraftResult{}, fmt.Errorf("DRAFT_COVER_INVALID: %w", err)
	}
	if cover.SHA256 != plan.CoverSHA256 {
		return DraftResult{}, errors.New("DRAFT_COVER_CHANGED: cover no longer matches the confirmed plan")
	}
	coverMediaID := mediaCache.Covers[cover.SHA256]
	if coverMediaID == "" {
		unlockCover, lockErr := workspace.NewLocal().LockArticle(ctx, plan.WorkspaceRoot, "media-object\x00"+accountStateKey+"\x00cover\x00"+cover.SHA256)
		if lockErr != nil {
			return DraftResult{}, lockErr
		}
		latest, loadErr := loadMediaCache(mediaCachePath, account, accountStateKey)
		if loadErr != nil {
			unlockCover()
			return DraftResult{}, loadErr
		}
		coverMediaID = latest.Covers[cover.SHA256]
		if coverMediaID == "" {
			uploaded, uploadErr := service.port.UploadCover(ctx, token, cover)
			if uploadErr != nil {
				unlockCover()
				return DraftResult{}, uploadErr
			}
			coverMediaID = strings.TrimSpace(uploaded.MediaID)
			if coverMediaID == "" {
				unlockCover()
				return DraftResult{}, errors.New("WECHAT_COVER_RESPONSE: media_id is required")
			}
			mediaCache.Covers[cover.SHA256] = coverMediaID
			if persistErr := persistMergedMediaCache("", "", cover.SHA256, coverMediaID); persistErr != nil {
				unlockCover()
				return DraftResult{}, fmt.Errorf("persist uploaded cover cache: %w", persistErr)
			}
		}
		unlockCover()
	}
	state.SchemaVersion = "1"
	state.Account = account
	state.AccountStateKey = accountStateKey
	state.SourcePath = plan.SourcePath
	state.SourceHash = plan.SourceHash
	state.ContentHash = plan.ContentHash
	state.LastPlanID = plan.ID
	state.Outcome = "submitting"
	state.UpdatedAt = time.Now().UTC()
	if err := persistDraftState(statePath, state); err != nil {
		return DraftResult{}, fmt.Errorf("persist uploaded media state: %w", err)
	}
	draft := WeChatDraft{
		Title: inspection.Title, Author: inspection.Author, Digest: inspection.Digest,
		Content: articleHTML, ThumbMediaID: coverMediaID, ContentSource: inspection.SourceURL,
	}
	mediaID := state.MediaID
	if plan.Operation == "update" {
		if err := service.port.UpdateDraft(ctx, token, WeChatDraftUpdate{MediaID: mediaID, Draft: draft}); err != nil {
			if isOutcomeUnknown(err) {
				return persistUnknownDraft(statePath, plan, state, mediaID, err)
			}
			if persistErr := persistKnownDraftFailure(statePath, state); persistErr != nil {
				return submittingStateResult(statePath, plan, mediaID, persistErr)
			}
			return DraftResult{}, err
		}
	} else {
		remote, err := service.port.AddDraft(ctx, token, draft)
		if err != nil {
			if isOutcomeUnknown(err) {
				return persistUnknownDraft(statePath, plan, state, mediaID, err)
			}
			if persistErr := persistKnownDraftFailure(statePath, state); persistErr != nil {
				return submittingStateResult(statePath, plan, mediaID, persistErr)
			}
			return DraftResult{}, err
		}
		mediaID = remote.MediaID
		if strings.TrimSpace(mediaID) == "" {
			return DraftResult{}, errors.New("WECHAT_DRAFT_RESPONSE: media_id is required")
		}
	}
	state.SchemaVersion = "1"
	state.Account = account
	state.AccountStateKey = accountStateKey
	state.SourcePath = plan.SourcePath
	state.SourceHash = plan.SourceHash
	state.ContentHash = plan.ContentHash
	state.MediaID = mediaID
	state.LastPlanID = plan.ID
	state.Outcome = "confirmed"
	state.UpdatedAt = time.Now().UTC()
	if err := persistDraftState(statePath, state); err != nil {
		return submittingStateResult(statePath, plan, mediaID, err)
	}
	return DraftResult{
		Outcome: "confirmed", Operation: plan.Operation, Account: account, MaskedAppID: plan.MaskedAppID,
		MediaID: mediaID, ContentHash: plan.ContentHash, StatePath: statePath,
	}, nil
}

func persistKnownDraftFailure(statePath string, state draftState) error {
	state.Outcome = "failed_known"
	state.UpdatedAt = time.Now().UTC()
	return persistDraftState(statePath, state)
}

func submittingStateResult(statePath string, plan DraftPlan, mediaID string, cause error) (DraftResult, error) {
	return DraftResult{
		Outcome: "outcome_unknown", Operation: plan.Operation, Account: plan.Account,
		MaskedAppID: plan.MaskedAppID, MediaID: mediaID, ContentHash: plan.ContentHash, StatePath: statePath,
	}, NewOutcomeUnknownError(cause)
}

func (service *Service) accessToken(ctx context.Context, credentials AccountCredentials, account, configPath string) (Token, error) {
	accountKey := credentialBinding(account, credentials)
	cachePath := filepath.Join(filepath.Dir(configPath), ".wechatloom-token-cache", accountKey+".json")
	if content, err := os.ReadFile(cachePath); err == nil {
		var cached tokenCacheRecord
		if json.Unmarshal(content, &cached) == nil && cached.SchemaVersion == "1" && cached.AccountKey == accountKey &&
			strings.TrimSpace(cached.Value) != "" && cached.ExpiresAt.After(time.Now().UTC().Add(time.Minute)) {
			return Token{Value: cached.Value, ExpiresIn: int(time.Until(cached.ExpiresAt).Seconds())}, nil
		}
	}
	token, err := service.port.AccessToken(ctx, credentials)
	if err != nil {
		return Token{}, err
	}
	if strings.TrimSpace(token.Value) == "" || token.ExpiresIn <= 0 {
		return Token{}, errors.New("WECHAT_TOKEN_RESPONSE: access_token and expires_in are required")
	}
	cached := tokenCacheRecord{
		SchemaVersion: "1", AccountKey: accountKey, Value: token.Value,
		ExpiresAt: time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second),
	}
	content, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return Token{}, fmt.Errorf("encode token cache: %w", err)
	}
	if err := atomicWritePrivate(cachePath, append(content, '\n')); err != nil {
		return Token{}, fmt.Errorf("persist token cache: %w", err)
	}
	return token, nil
}

func isOutcomeUnknown(err error) bool {
	var unknown *OutcomeUnknownError
	return errors.As(err, &unknown)
}

func persistUnknownDraft(statePath string, plan DraftPlan, state draftState, mediaID string, submitErr error) (DraftResult, error) {
	state.SchemaVersion = "1"
	state.Account = plan.Account
	state.SourceHash = plan.SourceHash
	state.ContentHash = plan.ContentHash
	state.MediaID = mediaID
	state.LastPlanID = plan.ID
	state.Outcome = "outcome_unknown"
	state.UpdatedAt = time.Now().UTC()
	if err := persistDraftState(statePath, state); err != nil {
		return DraftResult{}, fmt.Errorf("persist unknown draft outcome: %w", err)
	}
	return DraftResult{
		Outcome: "outcome_unknown", Operation: plan.Operation, Account: plan.Account,
		MaskedAppID: plan.MaskedAppID, MediaID: mediaID, ContentHash: plan.ContentHash, StatePath: statePath,
	}, submitErr
}

var localImagePattern = regexp.MustCompile(`src="(assets/[^"]+)"`)

func (service *Service) materializeContentImages(buildPath, html string, cache map[string]string, resolve func(MediaFile) (string, error)) (string, error) {
	matches := localImagePattern.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		media, err := readMediaFile(filepath.Join(buildPath, filepath.FromSlash(match[1])))
		if err != nil {
			return "", fmt.Errorf("read content image: %w", err)
		}
		remoteURL := cache[media.SHA256]
		if remoteURL == "" {
			remoteURL, err = resolve(media)
			if err != nil {
				return "", err
			}
			cache[media.SHA256] = remoteURL
		}
		html = strings.ReplaceAll(html, `src="`+match[1]+`"`, `src="`+remoteURL+`"`)
	}
	return html, nil
}

func loadMediaCache(path, account, accountStateKey string) (mediaCacheState, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return mediaCacheState{
			SchemaVersion: "1", Account: account, AccountStateKey: accountStateKey,
			ContentImages: map[string]string{}, Covers: map[string]string{},
		}, nil
	}
	if err != nil {
		return mediaCacheState{}, fmt.Errorf("read media cache: %w", err)
	}
	var cache mediaCacheState
	if err := json.Unmarshal(content, &cache); err != nil {
		return mediaCacheState{}, fmt.Errorf("decode media cache: %w", err)
	}
	if cache.SchemaVersion != "1" || cache.Account != account || cache.AccountStateKey != accountStateKey {
		return mediaCacheState{}, errors.New("MEDIA_CACHE_INVALID: account cache metadata does not match")
	}
	if cache.ContentImages == nil {
		cache.ContentImages = map[string]string{}
	}
	if cache.Covers == nil {
		cache.Covers = map[string]string{}
	}
	return cache, nil
}

func persistMediaCache(path string, cache mediaCacheState) error {
	content, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return atomicWritePrivate(path, append(content, '\n'))
}

func resolveCover(path string) (string, string, error) {
	if strings.TrimSpace(path) == "" {
		return "", "", nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("resolve cover: %w", err)
	}
	media, err := readMediaFile(absolute)
	if err != nil {
		return "", "", fmt.Errorf("DRAFT_COVER_INVALID: %w", err)
	}
	return absolute, media.SHA256, nil
}

func readMediaFile(path string) (MediaFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return MediaFile{}, err
	}
	if !info.Mode().IsRegular() {
		return MediaFile{}, errors.New("MEDIA_INVALID: image must be a regular file")
	}
	if info.Size() > maximumMediaBytes {
		return MediaFile{}, fmt.Errorf("MEDIA_SIZE: image exceeds %d bytes", maximumMediaBytes)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return MediaFile{}, err
	}
	mediaType := http.DetectContentType(content)
	if mediaType != "image/png" && mediaType != "image/jpeg" && mediaType != "image/gif" {
		return MediaFile{}, fmt.Errorf("unsupported image content type %q", mediaType)
	}
	configuration, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return MediaFile{}, fmt.Errorf("MEDIA_INVALID: decode image dimensions: %w", err)
	}
	if configuration.Width <= 0 || configuration.Height <= 0 || int64(configuration.Width)*int64(configuration.Height) > maximumMediaPixels {
		return MediaFile{}, errors.New("MEDIA_PIXELS: image dimensions exceed the safe pixel limit")
	}
	sum := sha256.Sum256(content)
	return MediaFile{Path: path, MediaType: mediaType, Content: content, SHA256: hex.EncodeToString(sum[:])}, nil
}

func loadDraftState(path string) (draftState, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return draftState{}, nil
	}
	if err != nil {
		return draftState{}, fmt.Errorf("read draft state: %w", err)
	}
	var state draftState
	if err := json.Unmarshal(content, &state); err != nil {
		return draftState{}, fmt.Errorf("decode draft state: %w", err)
	}
	return state, nil
}

func persistDraftState(path string, state draftState) error {
	content, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWritePrivate(path, append(content, '\n'))
}

func selectAccount(config userConfig, requested string) (string, AccountCredentials, error) {
	name := strings.TrimSpace(requested)
	if name == "" {
		name = strings.TrimSpace(config.WeChat.DefaultAccount)
	}
	if name == "" {
		return "", AccountCredentials{}, errors.New("ACCOUNT_NOT_SELECTED: account name or wechat.default_account is required")
	}
	credentials, ok := config.WeChat.Accounts[name]
	if !ok {
		return "", AccountCredentials{}, fmt.Errorf("ACCOUNT_NOT_FOUND: account %q is not configured", name)
	}
	credentials.AppID = strings.TrimSpace(credentials.AppID)
	credentials.AppSecret = strings.TrimSpace(credentials.AppSecret)
	if credentials.AppID == "" || credentials.AppSecret == "" {
		return "", AccountCredentials{}, fmt.Errorf("ACCOUNT_CREDENTIALS_INVALID: account %q requires app_id and app_secret", name)
	}
	return name, credentials, nil
}

func requireUserConfigOutsideProject(configPath, projectRoot string) error {
	resolvedConfig := configPath
	if evaluated, err := filepath.EvalSymlinks(configPath); err == nil {
		resolvedConfig = evaluated
	}
	resolvedRoot := projectRoot
	if evaluated, err := filepath.EvalSymlinks(projectRoot); err == nil {
		resolvedRoot = evaluated
	}
	if pathWithin(resolvedRoot, resolvedConfig) {
		return errors.New("USER_CONFIG_SCOPE: WeChat credentials must be stored outside the project directory")
	}
	return nil
}

func readAndValidateBuild(buildPath string) (buildManifest, error) {
	if err := builder.ValidatePreviewed(buildPath); err != nil {
		return buildManifest{}, err
	}
	manifestBytes, err := os.ReadFile(filepath.Join(buildPath, "manifest.json"))
	if err != nil {
		return buildManifest{}, fmt.Errorf("DRAFT_BUILD_INVALID: read manifest: %w", err)
	}
	var manifest buildManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return buildManifest{}, fmt.Errorf("DRAFT_BUILD_INVALID: decode manifest: %w", err)
	}
	if manifest.SchemaVersion != "1" || manifest.BuildID == "" || manifest.ContentHash == "" || manifest.Source.SHA256 == "" {
		return buildManifest{}, errors.New("DRAFT_BUILD_INVALID: manifest identity fields are required")
	}
	required := map[string]bool{"article.html": false, "preview.html": false}
	for _, artifact := range manifest.Artifacts {
		if _, ok := required[artifact.Path]; !ok {
			continue
		}
		content, err := os.ReadFile(filepath.Join(buildPath, artifact.Path))
		if err != nil {
			return buildManifest{}, fmt.Errorf("DRAFT_BUILD_INVALID: read %s: %w", artifact.Path, err)
		}
		sum := sha256.Sum256(content)
		if hex.EncodeToString(sum[:]) != artifact.SHA256 {
			return buildManifest{}, fmt.Errorf("DRAFT_BUILD_CHANGED: %s no longer matches the manifest", artifact.Path)
		}
		required[artifact.Path] = true
	}
	for name, found := range required {
		if !found {
			return buildManifest{}, fmt.Errorf("DRAFT_BUILD_INVALID: manifest does not record %s", name)
		}
	}
	return manifest, nil
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func draftStatePath(statePath, accountStateKey, sourcePath string) string {
	return filepath.Join(statePath, "drafts", stateKey(accountStateKey), stateKey(filepath.Clean(sourcePath))+".json")
}

func stateKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func credentialBinding(account string, credentials AccountCredentials) string {
	sum := sha256.Sum256([]byte(account + "\x00" + credentials.AppID + "\x00" + credentials.AppSecret))
	return hex.EncodeToString(sum[:])
}

func accountStateBinding(account string, credentials AccountCredentials) string {
	sum := sha256.Sum256([]byte(account + "\x00" + credentials.AppID))
	return hex.EncodeToString(sum[:])
}

func atomicWritePrivate(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".wechatloom-state-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
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
	remove = false
	return nil
}
