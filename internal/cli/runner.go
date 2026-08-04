package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wechatloom/wechatloom/internal/builder"
	"github.com/wechatloom/wechatloom/internal/catalog"
	"github.com/wechatloom/wechatloom/internal/preview"
	"github.com/wechatloom/wechatloom/internal/protocol"
	"github.com/wechatloom/wechatloom/internal/publisher"
	"github.com/wechatloom/wechatloom/internal/skillmanager"
	"github.com/wechatloom/wechatloom/internal/snapshot"
	"github.com/wechatloom/wechatloom/internal/updater"
	"github.com/wechatloom/wechatloom/internal/version"
	"github.com/wechatloom/wechatloom/internal/workspace"
)

type Runner struct {
	stdout    io.Writer
	stderr    io.Writer
	publisher *publisher.Service
}

func NewRunner(stdout, stderr io.Writer) *Runner {
	return &Runner{stdout: stdout, stderr: stderr, publisher: publisher.NewOfficial()}
}

func NewRunnerWithPublisher(stdout, stderr io.Writer, draftPublisher *publisher.Service) *Runner {
	return &Runner{stdout: stdout, stderr: stderr, publisher: draftPublisher}
}

func (runner *Runner) Run(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "capabilities":
			return runner.capabilities(slices.Contains(args[1:], "--json"))
		case "init":
			return runner.init(args[1:])
		case "inspect":
			return runner.inspect(args[1:])
		case "build":
			return runner.build(args[1:])
		case "theme":
			return runner.resource(args[1:], catalog.KindTheme)
		case "component":
			return runner.resource(args[1:], catalog.KindComponent)
		case "preview":
			return runner.preview(args[1:])
		case "snapshot":
			return runner.snapshot(args[1:])
		case "skill":
			return runner.skill(args[1:])
		case "account":
			return runner.account(args[1:])
		case "draft":
			return runner.draft(args[1:])
		case "doctor":
			return runner.doctor(args[1:])
		case "clean":
			return runner.clean(args[1:])
		case "plan":
			return runner.plan(args[1:])
		case "render":
			return runner.render(args[1:])
		case "update":
			return runner.update(args[1:])
		}
	}

	fmt.Fprintln(runner.stderr, "usage: wechatloom <init|inspect|plan|build|render|preview|snapshot|theme|component|skill|account|draft|doctor|clean|update|capabilities> [options]")
	return 2
}

func (runner *Runner) capabilities(asJSON bool) int {
	resourceCapabilities, err := catalog.NewBuiltin().Capabilities(context.Background())
	if err != nil {
		return runner.commandError(asJSON, "CAPABILITIES_ERROR", "Load capabilities", err)
	}
	data := struct {
		Commands []string `json:"commands"`
		Tool     struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"tool"`
		Themes       []catalog.Theme      `json:"themes"`
		Components   []catalog.Component  `json:"components"`
		RemoteWrites catalog.RemoteWrites `json:"remote_writes"`
	}{
		Commands:     []string{"init", "inspect", "plan", "build", "render", "preview", "snapshot", "theme", "component", "skill", "account", "draft", "doctor", "clean", "update", "capabilities"},
		Themes:       resourceCapabilities.Themes,
		Components:   resourceCapabilities.Components,
		RemoteWrites: resourceCapabilities.RemoteWrites,
	}
	data.Tool.Name = version.Name
	data.Tool.Version = version.Version

	if asJSON {
		if err := protocol.WriteJSON(
			runner.stdout,
			protocol.OK("CAPABILITIES_READY", "WeChatLoom capabilities are ready", "ready", data),
		); err != nil {
			fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
			return 1
		}
		return 0
	}

	fmt.Fprintln(runner.stdout, "WeChatLoom commands: init, inspect, plan, build, render, preview, snapshot, theme, component, skill, account, draft, doctor, clean, update, capabilities")
	return 0
}

func (runner *Runner) update(args []string) int {
	asJSON := slices.Contains(args, "--json")
	if len(args) == 0 || (args[0] != "check" && args[0] != "install") {
		return runner.usageError(asJSON, "update requires check or an explicitly confirmed install")
	}
	action := args[0]
	manifestURL := ""
	outputPath := ""
	confirmed := false
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--json":
		case "--confirm":
			confirmed = true
		case "--manifest-url":
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return runner.usageError(asJSON, "--manifest-url requires a URL")
			}
			manifestURL = args[index]
		case "--output":
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return runner.usageError(asJSON, "--output requires an executable path")
			}
			outputPath = args[index]
		default:
			return runner.usageError(asJSON, "update accepts only --manifest-url, --output, --confirm, and --json")
		}
	}
	if action == "check" && (confirmed || outputPath != "") {
		return runner.usageError(asJSON, "update check does not accept --confirm or --output")
	}
	if action == "install" && !confirmed {
		return runner.usageError(asJSON, "update install requires explicit --confirm")
	}
	result, err := updater.New().Check(context.Background(), manifestURL, version.Version)
	if err != nil {
		return runner.commandError(asJSON, "UPDATE_CHECK_FAILED", "Check for updates", err)
	}
	if action == "install" {
		if outputPath == "" {
			outputPath, err = os.Executable()
			if err != nil {
				return runner.commandError(asJSON, "UPDATE_INSTALL_FAILED", "Resolve current executable", err)
			}
		}
		installed, err := updater.New().Install(context.Background(), result, outputPath)
		if err != nil {
			return runner.commandError(asJSON, "UPDATE_INSTALL_FAILED", "Install verified update", err)
		}
		if asJSON {
			_ = protocol.WriteJSON(runner.stdout, protocol.OK("UPDATE_INSTALLED", "Verified update was atomically installed", "completed", installed))
			return 0
		}
		fmt.Fprintf(runner.stdout, "Installed WeChatLoom %s at %s\n", installed.Version, installed.Path)
		return 0
	}
	if asJSON {
		_ = protocol.WriteJSON(runner.stdout, protocol.OK("UPDATE_CHECKED", "Explicit update check completed", "completed", result))
		return 0
	}
	if result.Available {
		fmt.Fprintf(runner.stdout, "Update available: %s (current %s)\n", result.LatestVersion, result.CurrentVersion)
	} else {
		fmt.Fprintf(runner.stdout, "Already current: %s\n", result.CurrentVersion)
	}
	return 0
}

func (runner *Runner) render(args []string) int {
	asJSON := slices.Contains(args, "--json")
	sourcePath := ""
	root := ""
	theme := ""
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
		case "--root", "--theme":
			flag := args[index]
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return runner.usageError(asJSON, flag+" requires a value")
			}
			if flag == "--root" {
				root = args[index]
			} else {
				theme = args[index]
			}
		default:
			if strings.HasPrefix(args[index], "-") || sourcePath != "" {
				return runner.usageError(asJSON, "render requires exactly one Markdown file")
			}
			sourcePath = args[index]
		}
	}
	if sourcePath == "" {
		return runner.usageError(asJSON, "render requires exactly one Markdown file")
	}
	if root == "" {
		current, err := os.Getwd()
		if err != nil {
			return runner.commandError(asJSON, "WORKSPACE_ERROR", "Resolve project directory", err)
		}
		root = current
	}
	result, err := builder.New().Build(context.Background(), builder.BuildRequest{WorkspaceRoot: root, SourcePath: sourcePath, Theme: theme})
	if err != nil {
		return runner.commandError(asJSON, "RENDER_FAILED", "Render WeChat HTML", err)
	}
	if asJSON {
		_ = protocol.WriteJSON(runner.stdout, protocol.OK("RENDER_COMPLETED", "WeChat HTML was rendered locally", "completed", result))
		return 0
	}
	fmt.Fprintf(runner.stdout, "Rendered article: %s\n", result.ArticleHTMLPath)
	return 0
}

func (runner *Runner) plan(args []string) int {
	asJSON := slices.Contains(args, "--json")
	sourcePath := ""
	root := ""
	theme := ""
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
		case "--root", "--theme":
			flag := args[index]
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return runner.usageError(asJSON, flag+" requires a value")
			}
			if flag == "--root" {
				root = args[index]
			} else {
				theme = args[index]
			}
		default:
			if strings.HasPrefix(args[index], "-") || sourcePath != "" {
				return runner.usageError(asJSON, "plan requires exactly one Markdown file")
			}
			sourcePath = args[index]
		}
	}
	if sourcePath == "" {
		return runner.usageError(asJSON, "plan requires exactly one Markdown file")
	}
	if root == "" {
		current, err := os.Getwd()
		if err != nil {
			return runner.commandError(asJSON, "WORKSPACE_ERROR", "Resolve project directory", err)
		}
		root = current
	}
	result, err := builder.New().Build(context.Background(), builder.BuildRequest{WorkspaceRoot: root, SourcePath: sourcePath, Theme: theme})
	if err != nil {
		return runner.commandError(asJSON, "PLAN_FAILED", "Create layout plan", err)
	}
	data := struct {
		BuildPath      string `json:"build_path"`
		LayoutPlanPath string `json:"layout_plan_path"`
		ContentHash    string `json:"content_hash"`
	}{BuildPath: result.BuildPath, LayoutPlanPath: filepath.Join(result.BuildPath, "layout-plan.json"), ContentHash: result.ContentHash}
	if asJSON {
		_ = protocol.WriteJSON(runner.stdout, protocol.OK("PLAN_COMPLETED", "Validated layout plan was written", "completed", data))
		return 0
	}
	fmt.Fprintf(runner.stdout, "Layout plan: %s\n", data.LayoutPlanPath)
	return 0
}

func (runner *Runner) clean(args []string) int {
	asJSON := slices.Contains(args, "--json")
	root := ""
	confirmed := false
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
		case "--confirm":
			confirmed = true
		case "--root":
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return runner.usageError(asJSON, "--root requires a project directory")
			}
			root = args[index]
		default:
			return runner.usageError(asJSON, "clean accepts only --root, --confirm, and --json")
		}
	}
	if !confirmed {
		return runner.usageError(asJSON, "clean requires explicit --confirm; publishing state is preserved")
	}
	if root == "" {
		current, err := os.Getwd()
		if err != nil {
			return runner.commandError(asJSON, "WORKSPACE_ERROR", "Resolve project directory", err)
		}
		root = current
	}
	result, err := workspace.NewLocal().CleanBuilds(context.Background(), root)
	if err != nil {
		return runner.commandError(asJSON, "CLEAN_FAILED", "Clean build artifacts", err)
	}
	if asJSON {
		_ = protocol.WriteJSON(runner.stdout, protocol.OK("CLEAN_COMPLETED", "Build artifacts were removed; publishing state was preserved", "completed", result))
		return 0
	}
	fmt.Fprintf(runner.stdout, "Removed %d build artifact(s) from %s\n", result.Removed, result.Path)
	return 0
}

func (runner *Runner) doctor(args []string) int {
	asJSON := slices.Contains(args, "--json")
	root := ""
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
		case "--root":
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return runner.usageError(asJSON, "--root requires a project directory")
			}
			root = args[index]
		default:
			return runner.usageError(asJSON, "doctor accepts only --root and --json")
		}
	}
	if root == "" {
		current, err := os.Getwd()
		if err != nil {
			return runner.commandError(asJSON, "WORKSPACE_ERROR", "Resolve project directory", err)
		}
		root = current
	}
	resolved, err := workspace.NewLocal().Resolve(context.Background(), root)
	if err != nil {
		return runner.commandError(asJSON, "DOCTOR_FAILED", "Local workspace check failed", err)
	}
	browserAvailable := true
	if _, err := snapshot.Discover(); err != nil {
		browserAvailable = false
	}
	data := struct {
		Ready            bool   `json:"ready"`
		RemoteCalls      bool   `json:"remote_calls"`
		WorkspaceRoot    string `json:"workspace_root"`
		ConfigPath       string `json:"config_path"`
		BrowserAvailable bool   `json:"browser_available"`
	}{Ready: true, RemoteCalls: false, WorkspaceRoot: resolved.Root, ConfigPath: resolved.ConfigPath, BrowserAvailable: browserAvailable}
	if asJSON {
		_ = protocol.WriteJSON(runner.stdout, protocol.OK("DOCTOR_READY", "Local workspace is ready", "ready", data))
		return 0
	}
	fmt.Fprintf(runner.stdout, "Workspace ready: %s\n", resolved.Root)
	return 0
}

func (runner *Runner) draft(args []string) int {
	if len(args) != 0 && args[0] == "status" {
		return runner.draftStatus(args[1:])
	}
	if len(args) != 0 && args[0] == "reconcile" {
		return runner.draftReconcile(args[1:])
	}
	asJSON := slices.Contains(args, "--json")
	buildPath := ""
	root := ""
	configPath := ""
	account := ""
	coverPath := ""
	planPath := ""
	confirmation := ""
	dryRun := false
	newDraft := false
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
		case "--dry-run":
			dryRun = true
		case "--new-draft":
			newDraft = true
		case "--root", "--config", "--account", "--cover", "--plan", "--confirm":
			flag := args[index]
			index++
			if index >= len(args) || (flag != "--confirm" && strings.HasPrefix(args[index], "-")) {
				return runner.usageError(asJSON, flag+" requires a value")
			}
			switch flag {
			case "--root":
				root = args[index]
			case "--config":
				configPath = args[index]
			case "--account":
				account = args[index]
			case "--cover":
				coverPath = args[index]
			case "--plan":
				planPath = args[index]
			case "--confirm":
				confirmation = args[index]
			}
		default:
			if strings.HasPrefix(args[index], "-") || buildPath != "" {
				return runner.usageError(asJSON, "draft --dry-run requires exactly one committed build path")
			}
			buildPath = args[index]
		}
	}
	if dryRun && confirmation != "" {
		return runner.usageError(asJSON, "draft --dry-run and --confirm are mutually exclusive")
	}
	if confirmation != "" {
		if planPath == "" || buildPath != "" {
			return runner.usageError(asJSON, "draft --confirm requires --plan and no build path")
		}
		result, err := runner.publisher.Submit(context.Background(), publisher.ConfirmedDraftRequest{
			PlanPath: planPath, ConfirmationToken: confirmation, ConfigPath: configPath,
		})
		if err != nil {
			if result.Outcome == "outcome_unknown" && asJSON {
				_ = protocol.WriteJSON(runner.stdout, protocol.Failure(
					"DRAFT_OUTCOME_UNKNOWN",
					"Draft write outcome is unknown; inspect the WeChat draft list before reconciling",
					"outcome_unknown", false, result,
				))
				return 1
			}
			return runner.commandError(asJSON, "DRAFT_SUBMIT_FAILED", "Submit confirmed draft", err)
		}
		if asJSON {
			if err := protocol.WriteJSON(runner.stdout, protocol.OK("DRAFT_SUBMITTED", "Confirmed draft was submitted", "completed", result)); err != nil {
				fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
				return 1
			}
			return 0
		}
		fmt.Fprintf(runner.stdout, "Draft %s: %s (%s)\n", result.Operation, result.MediaID, result.Outcome)
		return 0
	}
	if !dryRun || buildPath == "" {
		return runner.usageError(asJSON, "draft requires an explicit --dry-run or --confirm")
	}
	if root == "" {
		current, err := os.Getwd()
		if err != nil {
			return runner.commandError(asJSON, "WORKSPACE_ERROR", "Resolve project directory", err)
		}
		root = current
	}
	if info, err := os.Stat(buildPath); err != nil || !info.IsDir() {
		previewedBuild, findErr := builder.FindPreviewedBuild(context.Background(), root, buildPath)
		if findErr != nil {
			return runner.commandError(asJSON, "DRAFT_PREVIEW_REQUIRED", "Find a previewed build for source", findErr)
		}
		buildPath = previewedBuild
	}
	plan, err := runner.publisher.Plan(context.Background(), publisher.DraftPlanRequest{
		WorkspaceRoot: root, BuildPath: buildPath, ConfigPath: configPath,
		Account: account, CoverPath: coverPath, NewDraft: newDraft,
	})
	if err != nil {
		return runner.commandError(asJSON, "DRAFT_PLAN_FAILED", "Create draft dry-run plan", err)
	}
	if asJSON {
		if err := protocol.WriteJSON(runner.stdout, protocol.OK("DRAFT_PLAN_READY", "Draft dry-run plan is ready for explicit confirmation", "planned", plan)); err != nil {
			fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(runner.stdout, "Draft plan %s: %s on account %s; confirm before %s\n", plan.ID, plan.Operation, plan.Account, plan.ExpiresAt.Format(time.RFC3339))
	return 0
}

func (runner *Runner) draftStatus(args []string) int {
	asJSON := slices.Contains(args, "--json")
	root := ""
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
		case "--root":
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return runner.usageError(asJSON, "--root requires a project directory")
			}
			root = args[index]
		default:
			return runner.usageError(asJSON, "draft status accepts only --root and --json")
		}
	}
	if root == "" {
		current, err := os.Getwd()
		if err != nil {
			return runner.commandError(asJSON, "WORKSPACE_ERROR", "Resolve project directory", err)
		}
		root = current
	}
	statuses, err := runner.publisher.ListDraftStates(context.Background(), root)
	if err != nil {
		return runner.commandError(asJSON, "DRAFT_STATUS_FAILED", "Read draft status", err)
	}
	if asJSON {
		_ = protocol.WriteJSON(runner.stdout, protocol.OK("DRAFT_STATUS_READY", "Local draft states are ready", "ready", statuses))
		return 0
	}
	for _, status := range statuses {
		fmt.Fprintf(runner.stdout, "%s\t%s\t%s\n", status.Outcome, status.Account, status.SourcePath)
	}
	return 0
}

func (runner *Runner) draftReconcile(args []string) int {
	asJSON := slices.Contains(args, "--json")
	planPath := ""
	result := ""
	mediaID := ""
	confirmed := false
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
		case "--confirm":
			confirmed = true
		case "--plan", "--result", "--media-id":
			flag := args[index]
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return runner.usageError(asJSON, flag+" requires a value")
			}
			switch flag {
			case "--plan":
				planPath = args[index]
			case "--result":
				result = args[index]
			case "--media-id":
				mediaID = args[index]
			}
		default:
			return runner.usageError(asJSON, "draft reconcile requires --plan, --result, and --confirm")
		}
	}
	if !confirmed || planPath == "" || result == "" {
		return runner.usageError(asJSON, "draft reconcile requires --plan, --result, and explicit --confirm")
	}
	status, err := runner.publisher.Reconcile(context.Background(), publisher.ReconcileRequest{PlanPath: planPath, Result: result, MediaID: mediaID})
	if err != nil {
		return runner.commandError(asJSON, "DRAFT_RECONCILE_FAILED", "Reconcile local draft state", err)
	}
	if asJSON {
		_ = protocol.WriteJSON(runner.stdout, protocol.OK("DRAFT_RECONCILED", "Local draft state was reconciled after manual WeChat inspection", "completed", status))
		return 0
	}
	fmt.Fprintf(runner.stdout, "Draft state reconciled: %s\n", status.Outcome)
	return 0
}

func (runner *Runner) account(args []string) int {
	asJSON := slices.Contains(args, "--json")
	filtered := make([]string, 0, len(args))
	configPath := ""
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
		case "--config":
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return runner.usageError(asJSON, "--config requires a file")
			}
			configPath = args[index]
		default:
			filtered = append(filtered, args[index])
		}
	}
	if len(filtered) < 1 || len(filtered) > 2 || filtered[0] != "verify" {
		return runner.usageError(asJSON, "account requires 'verify [account]'")
	}
	accountName := ""
	if len(filtered) == 2 {
		accountName = filtered[1]
	}
	readiness, err := runner.publisher.VerifyAccount(context.Background(), publisher.VerifyAccountRequest{
		ConfigPath: configPath,
		Account:    accountName,
	})
	if err != nil {
		return runner.commandError(asJSON, "ACCOUNT_VERIFY_FAILED", "Verify WeChat account", err)
	}
	if asJSON {
		if err := protocol.WriteJSON(runner.stdout, protocol.OK("ACCOUNT_VERIFIED", "WeChat account verified", "ready", readiness)); err != nil {
			fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(runner.stdout, "WeChat account %s is ready (%s)\n", readiness.Account, readiness.MaskedAppID)
	return 0
}

func (runner *Runner) skill(args []string) int {
	asJSON := slices.Contains(args, "--json")
	filtered := make([]string, 0, len(args))
	codexHome := ""
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
		case "--codex-home":
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return runner.usageError(asJSON, "--codex-home requires a directory")
			}
			codexHome = args[index]
		default:
			filtered = append(filtered, args[index])
		}
	}
	if len(filtered) != 2 || (filtered[0] != "status" && filtered[0] != "install" && filtered[0] != "update") || filtered[1] != "codex" {
		return runner.usageError(asJSON, "skill requires 'status codex', 'install codex', or 'update codex'")
	}
	resolvedHome, err := resolveCodexHome(codexHome)
	if err != nil {
		return runner.commandError(asJSON, "SKILL_STATUS_FAILED", "Resolve Codex home", err)
	}
	if filtered[0] == "install" {
		result, err := skillmanager.InstallCodex(context.Background(), resolvedHome)
		if err != nil {
			return runner.commandError(asJSON, "SKILL_INSTALL_FAILED", "Install Codex skill", err)
		}
		if asJSON {
			if err := protocol.WriteJSON(runner.stdout, protocol.OK("SKILL_INSTALLED", "Codex skill installed", "completed", result)); err != nil {
				fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
				return 1
			}
			return 0
		}
		fmt.Fprintf(runner.stdout, "Installed Codex skill %s at %s\n", result.InstalledVersion, result.Path)
		return 0
	}
	if filtered[0] == "update" {
		result, err := skillmanager.UpdateCodex(context.Background(), resolvedHome)
		if err != nil {
			return runner.commandError(asJSON, "SKILL_UPDATE_FAILED", "Update Codex skill", err)
		}
		if asJSON {
			if err := protocol.WriteJSON(runner.stdout, protocol.OK("SKILL_UPDATED", "Codex skill updated", "completed", result)); err != nil {
				fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
				return 1
			}
			return 0
		}
		fmt.Fprintf(runner.stdout, "Updated Codex skill from %s to %s at %s\n", result.PreviousVersion, result.InstalledVersion, result.Path)
		return 0
	}
	data, err := skillmanager.CodexStatus(resolvedHome)
	if err != nil {
		return runner.commandError(asJSON, "SKILL_STATUS_FAILED", "Read Codex skill status", err)
	}
	if asJSON {
		if err := protocol.WriteJSON(runner.stdout, protocol.OK("SKILL_STATUS_READY", "Codex skill status is ready", "ready", data)); err != nil {
			fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(runner.stdout, "Codex skill: %s (%s)\n", data.State, data.Path)
	return 0
}

func resolveCodexHome(explicit string) (string, error) {
	selected := strings.TrimSpace(explicit)
	if selected == "" {
		selected = strings.TrimSpace(os.Getenv("CODEX_HOME"))
	}
	if selected == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		selected = filepath.Join(home, ".codex")
	}
	return filepath.Abs(selected)
}

func (runner *Runner) preview(args []string) int {
	asJSON := slices.Contains(args, "--json")
	buildPath := ""
	port := 0
	open := true
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
		case "--no-open":
			open = false
		case "--port":
			index++
			if index >= len(args) {
				return runner.usageError(asJSON, "--port requires a number")
			}
			parsed, err := strconv.Atoi(args[index])
			if err != nil {
				return runner.usageError(asJSON, "--port requires a number")
			}
			port = parsed
		default:
			if strings.HasPrefix(args[index], "-") || buildPath != "" {
				return runner.usageError(asJSON, "preview requires exactly one completed build directory")
			}
			buildPath = args[index]
		}
	}
	if buildPath == "" {
		return runner.usageError(asJSON, "preview requires exactly one completed build directory")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	session, err := preview.Start(ctx, preview.StartRequest{BuildPath: buildPath, Port: port})
	if err != nil {
		return runner.commandError(asJSON, "PREVIEW_FAILED", "Start local preview", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = session.Close(closeCtx)
	}()
	data := struct {
		URL      string `json:"url"`
		ReadOnly bool   `json:"read_only"`
	}{URL: session.URL(), ReadOnly: true}
	if asJSON {
		if err := protocol.WriteJSON(runner.stdout, protocol.OK("PREVIEW_READY", "Local preview is ready", "ready", data)); err != nil {
			fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
			return 1
		}
	} else {
		fmt.Fprintf(runner.stdout, "Preview: %s\nPress Ctrl+C to stop.\n", session.URL())
	}
	if open {
		if err := openBrowser(session.URL()); err != nil {
			fmt.Fprintf(runner.stderr, "open browser: %v\n", err)
		}
	}
	<-ctx.Done()
	if err := builder.MarkPreviewed(buildPath); err != nil {
		return runner.commandError(asJSON, "PREVIEW_RECEIPT_FAILED", "Record completed preview", err)
	}
	return 0
}

func openBrowser(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	return command.Start()
}

func (runner *Runner) snapshot(args []string) int {
	asJSON := slices.Contains(args, "--json")
	buildPath := ""
	outputDirectory := ""
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
		case "--output":
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return runner.usageError(asJSON, "--output requires a directory")
			}
			outputDirectory = args[index]
		default:
			if strings.HasPrefix(args[index], "-") || buildPath != "" {
				return runner.usageError(asJSON, "snapshot requires exactly one completed build directory")
			}
			buildPath = args[index]
		}
	}
	if buildPath == "" {
		return runner.usageError(asJSON, "snapshot requires exactly one completed build directory")
	}
	if outputDirectory == "" {
		outputDirectory = filepath.Join(buildPath, "snapshots")
	}
	browser, err := snapshot.Discover()
	if err != nil {
		return runner.commandError(asJSON, "BROWSER_NOT_FOUND", "Create PNG snapshots", err)
	}
	result, err := snapshot.CaptureMobileSet(context.Background(), browser, snapshot.SetRequest{
		ArticleHTMLPath: filepath.Join(buildPath, "article.html"),
		OutputDirectory: outputDirectory,
	})
	if err != nil {
		return runner.commandError(asJSON, "SNAPSHOT_FAILED", "Create PNG snapshots", err)
	}
	if err := builder.MarkPreviewed(buildPath); err != nil {
		return runner.commandError(asJSON, "PREVIEW_RECEIPT_FAILED", "Record completed snapshot preview", err)
	}
	if asJSON {
		if err := protocol.WriteJSON(runner.stdout, protocol.OK("SNAPSHOTS_CREATED", "Mobile snapshots created", "completed", result)); err != nil {
			fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
			return 1
		}
		return 0
	}
	for _, path := range result.Snapshots {
		fmt.Fprintln(runner.stdout, path)
	}
	return 0
}

func (runner *Runner) resource(args []string, kind catalog.Kind) int {
	asJSON := slices.Contains(args, "--json")
	if kind == catalog.KindTheme && len(args) > 0 {
		switch args[0] {
		case "export", "validate", "install":
			return runner.themePackage(args)
		}
	}
	if kind == catalog.KindComponent && len(args) > 0 && args[0] == "export" {
		return runner.exportComponents(args[1:], asJSON)
	}
	filtered := make([]string, 0, len(args))
	root := ""
	for index := 0; index < len(args); index++ {
		if args[index] == "--json" {
			continue
		}
		if args[index] == "--root" {
			index++
			if kind != catalog.KindTheme || index >= len(args) || strings.HasPrefix(args[index], "-") {
				return runner.usageError(asJSON, "--root requires a project directory for theme discovery")
			}
			root = args[index]
			continue
		}
		filtered = append(filtered, args[index])
	}
	var resources catalog.Catalog = catalog.NewBuiltin()
	if root != "" {
		resources = catalog.NewProject(root)
	}
	label := string(kind)
	if len(filtered) == 1 && filtered[0] == "list" {
		result, err := resources.List(context.Background(), catalog.Query{Kind: kind})
		if err != nil {
			return runner.commandError(asJSON, strings.ToUpper(label)+"_LIST_FAILED", "List "+label+" resources", err)
		}
		if asJSON {
			code := strings.ToUpper(label) + "S_LISTED"
			if err := protocol.WriteJSON(runner.stdout, protocol.OK(code, titleWord(label)+" resources listed", "ready", result)); err != nil {
				fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
				return 1
			}
			return 0
		}
		for _, resource := range result.Resources {
			fmt.Fprintf(runner.stdout, "%s\t%s\t%s\n", resource.Name, resource.Family, resource.Version)
		}
		return 0
	}
	if len(filtered) == 2 && filtered[0] == "show" {
		definition, err := resources.Show(context.Background(), catalog.Ref{Kind: kind, Name: filtered[1]})
		if err != nil {
			return runner.commandError(asJSON, strings.ToUpper(label)+"_NOT_FOUND", "Show "+label, err)
		}
		if asJSON {
			code := strings.ToUpper(label) + "_SHOWN"
			if err := protocol.WriteJSON(runner.stdout, protocol.OK(code, titleWord(label)+" definition ready", "ready", definition)); err != nil {
				fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
				return 1
			}
			return 0
		}
		encoded, err := json.MarshalIndent(definition, "", "  ")
		if err != nil {
			return runner.commandError(false, strings.ToUpper(label)+"_SHOW_FAILED", "Encode "+label, err)
		}
		fmt.Fprintln(runner.stdout, string(encoded))
		return 0
	}
	return runner.usageError(asJSON, label+" requires 'list' or 'show <name>'")
}

func (runner *Runner) exportComponents(args []string, asJSON bool) int {
	outputDirectory := ""
	name := ""
	all := false
	force := false
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
		case "--all":
			all = true
		case "--force":
			force = true
		case "--output":
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return runner.usageError(asJSON, "--output requires a directory")
			}
			outputDirectory = args[index]
		default:
			if strings.HasPrefix(args[index], "-") || name != "" {
				return runner.usageError(asJSON, "component export accepts one component name or --all")
			}
			name = args[index]
		}
	}
	if outputDirectory == "" || (all == (name != "")) {
		return runner.usageError(asJSON, "component export requires one component name or --all and --output")
	}
	builtin := catalog.NewBuiltin()
	names := []string{name}
	if all {
		listed, err := builtin.List(context.Background(), catalog.Query{Kind: catalog.KindComponent})
		if err != nil {
			return runner.commandError(asJSON, "COMPONENT_EXPORT_FAILED", "List components", err)
		}
		names = names[:0]
		for _, resource := range listed.Resources {
			names = append(names, resource.Name)
		}
	}
	paths := make([]string, 0, len(names)*3)
	for _, selected := range names {
		definition, err := builtin.Show(context.Background(), catalog.Ref{Kind: catalog.KindComponent, Name: selected})
		if err != nil {
			return runner.commandError(asJSON, "COMPONENT_EXPORT_FAILED", "Resolve component", err)
		}
		schema, err := json.MarshalIndent(catalog.ComponentJSONSchema(*definition.Component), "", "  ")
		if err != nil {
			return runner.commandError(asJSON, "COMPONENT_EXPORT_FAILED", "Encode component schema", err)
		}
		files := map[string][]byte{
			filepath.Join(selected, "schema.json"):            append(schema, '\n'),
			filepath.Join(selected, "examples", "valid.md"):   []byte(definition.Component.ValidExample),
			filepath.Join(selected, "examples", "invalid.md"): []byte(definition.Component.InvalidExample),
		}
		fileNames := make([]string, 0, len(files))
		for relative := range files {
			fileNames = append(fileNames, relative)
		}
		slices.Sort(fileNames)
		for _, relative := range fileNames {
			target := filepath.Join(outputDirectory, relative)
			if !force {
				if _, err := os.Stat(target); err == nil {
					return runner.commandError(asJSON, "COMPONENT_EXPORT_CONFLICT", "Export component", fmt.Errorf("%s already exists; use --force to replace it", target))
				}
			}
			if err := atomicWriteFile(target, files[relative], 0o644); err != nil {
				return runner.commandError(asJSON, "COMPONENT_EXPORT_FAILED", "Write component assets", err)
			}
			absolute, _ := filepath.Abs(target)
			paths = append(paths, absolute)
		}
	}
	data := struct {
		Files []string `json:"files"`
	}{Files: paths}
	if asJSON {
		if err := protocol.WriteJSON(runner.stdout, protocol.OK("COMPONENTS_EXPORTED", "Component schemas and examples exported", "completed", data)); err != nil {
			fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
			return 1
		}
	} else {
		for _, path := range paths {
			fmt.Fprintln(runner.stdout, path)
		}
	}
	return 0
}

func (runner *Runner) themePackage(args []string) int {
	asJSON := slices.Contains(args, "--json")
	switch args[0] {
	case "export":
		return runner.exportThemes(args[1:], asJSON)
	case "validate":
		path, ok := onePositional(args[1:], "--json")
		if !ok {
			return runner.usageError(asJSON, "theme validate requires exactly one theme.json file")
		}
		packageFile, err := catalog.DecodeThemePackage(path)
		if err != nil {
			return runner.commandError(asJSON, "THEME_INVALID", "Theme package is invalid", err)
		}
		data := struct {
			Name     string `json:"name"`
			Version  string `json:"version"`
			DataOnly bool   `json:"data_only"`
		}{Name: packageFile.Theme.Name, Version: packageFile.Theme.Version, DataOnly: true}
		if asJSON {
			if err := protocol.WriteJSON(runner.stdout, protocol.OK("THEME_VALID", "Theme package is valid", "ready", data)); err != nil {
				fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
				return 1
			}
		} else {
			fmt.Fprintf(runner.stdout, "Valid theme: %s %s\n", data.Name, data.Version)
		}
		return 0
	case "install":
		return runner.installTheme(args[1:], asJSON)
	default:
		return runner.usageError(asJSON, "unsupported theme package command")
	}
}

func (runner *Runner) exportThemes(args []string, asJSON bool) int {
	outputDirectory := ""
	name := ""
	all := false
	force := false
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
		case "--all":
			all = true
		case "--force":
			force = true
		case "--output":
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return runner.usageError(asJSON, "--output requires a directory")
			}
			outputDirectory = args[index]
		default:
			if strings.HasPrefix(args[index], "-") || name != "" {
				return runner.usageError(asJSON, "theme export accepts one theme name or --all")
			}
			name = args[index]
		}
	}
	if outputDirectory == "" || (all == (name != "")) {
		return runner.usageError(asJSON, "theme export requires one theme name or --all and --output")
	}
	builtin := catalog.NewBuiltin()
	names := []string{name}
	if all {
		listed, err := builtin.List(context.Background(), catalog.Query{Kind: catalog.KindTheme})
		if err != nil {
			return runner.commandError(asJSON, "THEME_EXPORT_FAILED", "List themes", err)
		}
		names = names[:0]
		for _, resource := range listed.Resources {
			names = append(names, resource.Name)
		}
	}
	exported := make([]string, 0, len(names))
	for _, selected := range names {
		definition, err := builtin.Show(context.Background(), catalog.Ref{Kind: catalog.KindTheme, Name: selected})
		if err != nil {
			return runner.commandError(asJSON, "THEME_EXPORT_FAILED", "Resolve theme", err)
		}
		encoded, err := json.MarshalIndent(catalog.ThemePackage{SchemaVersion: "1", Theme: *definition.Theme}, "", "  ")
		if err != nil {
			return runner.commandError(asJSON, "THEME_EXPORT_FAILED", "Encode theme", err)
		}
		encoded = append(encoded, '\n')
		target := filepath.Join(outputDirectory, selected, "theme.json")
		if !force {
			if _, err := os.Stat(target); err == nil {
				return runner.commandError(asJSON, "THEME_EXPORT_CONFLICT", "Export theme", fmt.Errorf("%s already exists; use --force to replace it", target))
			}
		}
		if err := atomicWriteFile(target, encoded, 0o644); err != nil {
			return runner.commandError(asJSON, "THEME_EXPORT_FAILED", "Write theme package", err)
		}
		absolute, _ := filepath.Abs(target)
		exported = append(exported, absolute)
	}
	data := struct {
		Packages []string `json:"packages"`
	}{Packages: exported}
	if asJSON {
		if err := protocol.WriteJSON(runner.stdout, protocol.OK("THEMES_EXPORTED", "Theme packages exported", "completed", data)); err != nil {
			fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
			return 1
		}
	} else {
		for _, path := range exported {
			fmt.Fprintln(runner.stdout, path)
		}
	}
	return 0
}

func (runner *Runner) installTheme(args []string, asJSON bool) int {
	packagePath := ""
	root := ""
	force := false
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
		case "--force":
			force = true
		case "--root":
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return runner.usageError(asJSON, "--root requires a project directory")
			}
			root = args[index]
		default:
			if strings.HasPrefix(args[index], "-") || packagePath != "" {
				return runner.usageError(asJSON, "theme install requires exactly one theme.json file")
			}
			packagePath = args[index]
		}
	}
	if packagePath == "" {
		return runner.usageError(asJSON, "theme install requires exactly one theme.json file")
	}
	if root == "" {
		current, err := os.Getwd()
		if err != nil {
			return runner.commandError(asJSON, "WORKSPACE_ERROR", "Resolve project directory", err)
		}
		root = current
	}
	resolved, err := workspace.NewLocal().Resolve(context.Background(), root)
	if err != nil {
		return runner.commandError(asJSON, "WORKSPACE_ERROR", "Resolve project directory", err)
	}
	packageFile, err := catalog.DecodeThemePackage(packagePath)
	if err != nil {
		return runner.commandError(asJSON, "THEME_INVALID", "Theme package is invalid", err)
	}
	encoded, err := json.MarshalIndent(packageFile, "", "  ")
	if err != nil {
		return runner.commandError(asJSON, "THEME_INSTALL_FAILED", "Encode theme package", err)
	}
	encoded = append(encoded, '\n')
	target := filepath.Join(resolved.Root, ".wechatloom", "themes", packageFile.Theme.Name, "theme.json")
	if existing, err := os.ReadFile(target); err == nil {
		if string(existing) == string(encoded) {
			force = true
		} else if !force {
			return runner.commandError(asJSON, "THEME_VERSION_CONFLICT", "Install theme", fmt.Errorf("theme %q is already installed; use --force to replace it", packageFile.Theme.Name))
		}
	}
	if err := atomicWriteFile(target, encoded, 0o644); err != nil {
		return runner.commandError(asJSON, "THEME_INSTALL_FAILED", "Install theme", err)
	}
	data := struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Path    string `json:"path"`
	}{Name: packageFile.Theme.Name, Version: packageFile.Theme.Version, Path: target}
	if asJSON {
		if err := protocol.WriteJSON(runner.stdout, protocol.OK("THEME_INSTALLED", "Theme package installed", "completed", data)); err != nil {
			fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
			return 1
		}
	} else {
		fmt.Fprintf(runner.stdout, "Installed theme %s %s\n", data.Name, data.Version)
	}
	return 0
}

func atomicWriteFile(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".wechatloom-theme-*")
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
	if err := temporary.Chmod(mode); err != nil {
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
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	remove = false
	return nil
}

func (runner *Runner) init(args []string) int {
	asJSON := slices.Contains(args, "--json")
	root := ""
	for _, arg := range args {
		if arg != "--json" {
			if root != "" {
				fmt.Fprintln(runner.stderr, "init accepts at most one project directory")
				return 2
			}
			root = arg
		}
	}
	if root == "" {
		currentDirectory, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(runner.stderr, "resolve current directory: %v\n", err)
			return 1
		}
		root = currentDirectory
	}

	resolved, err := workspace.NewLocal().Init(context.Background(), root)
	if err != nil {
		fmt.Fprintf(runner.stderr, "initialize workspace: %v\n", err)
		return 1
	}

	if asJSON {
		if err := protocol.WriteJSON(
			runner.stdout,
			protocol.OK(
				"WORKSPACE_INITIALIZED",
				"WeChatLoom workspace is ready",
				"ready",
				resolved,
			),
		); err != nil {
			fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
			return 1
		}
		return 0
	}

	fmt.Fprintf(runner.stdout, "Initialized WeChatLoom workspace in %s\n", resolved.Root)
	return 0
}

func (runner *Runner) inspect(args []string) int {
	asJSON := slices.Contains(args, "--json")
	sourcePath, ok := onePositional(args, "--json")
	if !ok {
		return runner.usageError(asJSON, "inspect requires exactly one Markdown file")
	}

	inspection, err := builder.New().Inspect(context.Background(), builder.InspectRequest{
		SourcePath: sourcePath,
	})
	if err != nil {
		return runner.commandError(asJSON, "INSPECTION_ERROR", "Inspection failed", err)
	}
	if len(inspection.Errors) != 0 {
		if asJSON {
			_ = protocol.WriteJSON(
				runner.stdout,
				protocol.Failure(
					"INSPECTION_FAILED",
					"Article is not ready",
					"invalid",
					false,
					inspection,
				),
			)
		} else {
			fmt.Fprintf(runner.stderr, "Article is not ready: %s\n", inspection.Errors[0].Message)
		}
		return 1
	}

	if asJSON {
		if err := protocol.WriteJSON(
			runner.stdout,
			protocol.OK(
				"INSPECTION_COMPLETED",
				"Article inspection completed",
				"ready",
				inspection,
			),
		); err != nil {
			fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
			return 1
		}
		return 0
	}

	fmt.Fprintf(
		runner.stdout,
		"Ready: %s (%d component)\n",
		inspection.Title,
		inspection.ComponentCount,
	)
	return 0
}

func titleWord(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func (runner *Runner) build(args []string) int {
	asJSON := slices.Contains(args, "--json")
	sourcePath := ""
	root := ""
	theme := ""
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
		case "--root":
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return runner.usageError(asJSON, "--root requires a project directory")
			}
			root = args[index]
		case "--theme":
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "-") {
				return runner.usageError(asJSON, "--theme requires a theme name")
			}
			theme = args[index]
		default:
			if strings.HasPrefix(args[index], "--root=") {
				root = strings.TrimPrefix(args[index], "--root=")
				continue
			}
			if strings.HasPrefix(args[index], "--theme=") {
				theme = strings.TrimPrefix(args[index], "--theme=")
				if theme == "" {
					return runner.usageError(asJSON, "--theme requires a theme name")
				}
				continue
			}
			if strings.HasPrefix(args[index], "-") || sourcePath != "" {
				return runner.usageError(asJSON, "build requires exactly one Markdown file")
			}
			sourcePath = args[index]
		}
	}
	if sourcePath == "" {
		return runner.usageError(asJSON, "build requires exactly one Markdown file")
	}
	if root == "" {
		currentDirectory, err := os.Getwd()
		if err != nil {
			return runner.commandError(asJSON, "WORKSPACE_ERROR", "Resolve project directory", err)
		}
		root = currentDirectory
	}

	result, err := builder.New().Build(context.Background(), builder.BuildRequest{
		WorkspaceRoot: root,
		SourcePath:    sourcePath,
		Theme:         theme,
	})
	if err != nil {
		return runner.commandError(asJSON, "BUILD_FAILED", "Build failed", err)
	}

	if asJSON {
		if err := protocol.WriteJSON(
			runner.stdout,
			protocol.OK(
				"BUILD_COMPLETED",
				"Local article build completed",
				"completed",
				result,
			),
		); err != nil {
			fmt.Fprintf(runner.stderr, "write JSON: %v\n", err)
			return 1
		}
		return 0
	}

	fmt.Fprintf(runner.stdout, "Built article: %s\n", result.ArticleHTMLPath)
	return 0
}

func onePositional(args []string, acceptedFlags ...string) (string, bool) {
	accepted := make(map[string]bool, len(acceptedFlags))
	for _, flag := range acceptedFlags {
		accepted[flag] = true
	}

	positional := ""
	for _, arg := range args {
		if accepted[arg] {
			continue
		}
		if strings.HasPrefix(arg, "-") || positional != "" {
			return "", false
		}
		positional = arg
	}
	return positional, positional != ""
}

func (runner *Runner) usageError(asJSON bool, message string) int {
	if asJSON {
		_ = protocol.WriteJSON(
			runner.stdout,
			protocol.Failure("INVALID_ARGUMENTS", message, "invalid", false, nil),
		)
	} else {
		fmt.Fprintln(runner.stderr, message)
	}
	return 2
}

func (runner *Runner) commandError(asJSON bool, code, message string, commandErr error) int {
	if asJSON {
		_ = protocol.WriteJSON(
			runner.stdout,
			protocol.Failure(code, message, "failed", false, map[string]string{
				"error": commandErr.Error(),
			}),
		)
	} else {
		fmt.Fprintf(runner.stderr, "%s: %v\n", message, commandErr)
	}
	return 1
}
