package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wechatloom/wechatloom/internal/workspace"
)

type DraftStatus struct {
	Account     string    `json:"account"`
	SourcePath  string    `json:"source_path"`
	ContentHash string    `json:"content_hash"`
	MediaID     string    `json:"media_id,omitempty"`
	Outcome     string    `json:"outcome"`
	UpdatedAt   time.Time `json:"updated_at"`
	StatePath   string    `json:"state_path"`
}

type ReconcileRequest struct {
	PlanPath string
	Result   string
	MediaID  string
}

func (service *Service) ListDraftStates(ctx context.Context, workspaceRoot string) ([]DraftStatus, error) {
	resolved, err := workspace.NewLocal().Resolve(ctx, workspaceRoot)
	if err != nil {
		return nil, err
	}
	paths, err := filepath.Glob(filepath.Join(resolved.StatePath, "drafts", "*", "*.json"))
	if err != nil {
		return nil, fmt.Errorf("list draft states: %w", err)
	}
	statuses := make([]DraftStatus, 0, len(paths))
	for _, path := range paths {
		state, err := loadDraftState(path)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, statusFromState(path, state))
	}
	sort.Slice(statuses, func(left, right int) bool {
		return statuses[left].UpdatedAt.After(statuses[right].UpdatedAt)
	})
	return statuses, nil
}

func (service *Service) Reconcile(ctx context.Context, request ReconcileRequest) (DraftStatus, error) {
	content, err := os.ReadFile(request.PlanPath)
	if err != nil {
		return DraftStatus{}, fmt.Errorf("DRAFT_PLAN_INVALID: read plan: %w", err)
	}
	var plan DraftPlan
	if err := json.Unmarshal(content, &plan); err != nil {
		return DraftStatus{}, fmt.Errorf("DRAFT_PLAN_INVALID: decode plan: %w", err)
	}
	resolved, err := workspace.NewLocal().Resolve(ctx, plan.WorkspaceRoot)
	if err != nil {
		return DraftStatus{}, err
	}
	absolutePlan, err := filepath.Abs(request.PlanPath)
	if err != nil || absolutePlan != plan.PlanPath || !pathWithin(filepath.Join(resolved.StatePath, "plans"), absolutePlan) {
		return DraftStatus{}, errors.New("DRAFT_PLAN_INVALID: plan is outside the workspace state directory")
	}
	unlock, err := workspace.NewLocal().LockArticle(ctx, plan.WorkspaceRoot, plan.Account+"\x00"+plan.SourcePath)
	if err != nil {
		return DraftStatus{}, err
	}
	defer unlock()
	if strings.TrimSpace(plan.AccountStateKey) == "" {
		return DraftStatus{}, errors.New("DRAFT_PLAN_INVALID: account state identity is required")
	}
	statePath := draftStatePath(resolved.StatePath, plan.AccountStateKey, plan.SourcePath)
	state, err := loadDraftState(statePath)
	if err != nil {
		return DraftStatus{}, err
	}
	if state.Outcome != "outcome_unknown" && state.Outcome != "submitting" {
		return DraftStatus{}, errors.New("DRAFT_RECONCILIATION_NOT_REQUIRED: draft state is not unknown")
	}
	if state.AccountStateKey != "" && state.AccountStateKey != plan.AccountStateKey {
		return DraftStatus{}, errors.New("DRAFT_STATE_INVALID: account identity does not match")
	}
	switch strings.TrimSpace(request.Result) {
	case "confirmed":
		mediaID := strings.TrimSpace(request.MediaID)
		if mediaID == "" {
			mediaID = state.MediaID
		}
		if mediaID == "" {
			return DraftStatus{}, errors.New("DRAFT_RECONCILIATION_MEDIA_ID: confirmed result requires the media_id observed in WeChat")
		}
		state.MediaID = mediaID
		state.ContentHash = plan.ContentHash
		state.Outcome = "confirmed"
	case "absent":
		if plan.Operation == "add" {
			state.MediaID = ""
		}
		state.Outcome = "failed_known"
	default:
		return DraftStatus{}, errors.New("DRAFT_RECONCILIATION_RESULT: result must be confirmed or absent")
	}
	state.LastPlanID = plan.ID
	state.AccountStateKey = plan.AccountStateKey
	state.UpdatedAt = time.Now().UTC()
	if err := persistDraftState(statePath, state); err != nil {
		return DraftStatus{}, fmt.Errorf("persist reconciled draft state: %w", err)
	}
	return statusFromState(statePath, state), nil
}

func statusFromState(path string, state draftState) DraftStatus {
	return DraftStatus{
		Account: state.Account, SourcePath: state.SourcePath, ContentHash: state.ContentHash,
		MediaID: state.MediaID, Outcome: state.Outcome, UpdatedAt: state.UpdatedAt, StatePath: path,
	}
}
