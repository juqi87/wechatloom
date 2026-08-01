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
	"github.com/wechatloom/wechatloom/internal/snapshot"
	"github.com/wechatloom/wechatloom/internal/version"
	"github.com/wechatloom/wechatloom/internal/workspace"
)

type Runner struct {
	stdout io.Writer
	stderr io.Writer
}

func NewRunner(stdout, stderr io.Writer) *Runner {
	return &Runner{stdout: stdout, stderr: stderr}
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
		}
	}

	fmt.Fprintln(runner.stderr, "usage: wechatloom <init|inspect|build|preview|snapshot|theme|component|capabilities> [options]")
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
		Commands:     []string{"init", "inspect", "build", "preview", "snapshot", "theme", "component", "capabilities"},
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

	fmt.Fprintln(runner.stdout, "WeChatLoom commands: init, inspect, build, preview, snapshot, theme, component, capabilities")
	return 0
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
