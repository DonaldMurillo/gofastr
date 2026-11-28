package initcmd

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	goversion "go/version"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/agentscmd"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/projectfs"
	"github.com/DonaldMurillo/gofastr/core/settings"
	"github.com/DonaldMurillo/gofastr/internal/dsnredact"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

// RunInit implements `gofastr init <name|.>`, and with --reinit refreshes
// only the agent onboarding files.
func RunInit(args []string) {
	if cli.HasHelpFlag(args) {
		printInitHelp()
		return
	}
	name, flags, err := splitInitArgs(args)
	if err != nil {
		cli.UsageFail("%v\n  Run 'gofastr init --help' for the accepted flags.", err)
		return
	}

	modulePath := ""
	dbDriver := "sqlite3"
	noEntity := false
	reinit := false
	force := false

	for _, arg := range flags {
		switch {
		case arg == "--reinit":
			reinit = true
		case arg == "--force":
			force = true
		case strings.HasPrefix(arg, "--module="):
			modulePath = strings.TrimPrefix(arg, "--module=")
			if modulePath == "" {
				cli.Fail("--module requires a non-empty value.")
				cli.OsExit(1)
			}
			if err := module.CheckImportPath(modulePath); err != nil {
				cli.Fail("--module=%q is not a valid Go module path: %v", modulePath, err)
				cli.Info("This invocation has not written any files. Example: --module=github.com/you/app")
				cli.OsExit(1)
			}
		case arg == "--no-entity":
			noEntity = true
		case strings.HasPrefix(arg, "--db="):
			switch driver := project.CanonicalDBDriver(strings.TrimPrefix(arg, "--db=")); driver {
			case "sqlite3", "postgres":
				dbDriver = driver
			default:
				choice := strings.TrimPrefix(arg, "--db=")
				cli.Fail("--db=%q is not a recognized database driver. Accepted values: sqlite, postgres (aliases: sqlite3, postgresql).", choice)
				cli.OsExit(1)
			}
		default:
			cli.UsageFail("Unknown flag %q.\n  Run 'gofastr init --help' for the accepted flags.", arg)
		}
	}

	if name == "" {
		cli.UsageFail("Project name is required.\n  Usage: gofastr init <project-name>\n  Example: gofastr init myapp")
	}

	if err := validateProjectName(name); err != nil {
		cli.Fail("Invalid project name %q: %v", name, err)
		cli.Info("Use lowercase letters, digits, and hyphens. Example: my-blog-app")
		cli.OsExit(1)
	}

	if reinit {
		if info, err := os.Stat(name); err != nil || !info.IsDir() {
			cli.Fail("--reinit refreshes an existing project, and %q is not a directory.", name)
			cli.Info("This invocation has not written any files. Check the name, or run `gofastr init %s` to create the project.", name)
			cli.OsExit(1)
			return
		}
		runReinit(name, force)
		return
	}

	projName := name
	if name != "." {
		if _, err := os.Stat(name); err == nil {
			cli.Fail("Directory %q already exists.", name)
			cli.Info("This invocation has not written any files. Check whether an earlier run created the project.")
			cli.Info("Use a different name for a new project, or inspect the existing directory before running init again.")
			cli.OsExit(1)
		}
	} else {
		if envTrackedByGit(".") {
			cli.Fail(".env is tracked by git; init would write a signing key into it.")
			cli.Info("Untrack it first: git rm --cached .env")
			cli.OsExit(1)
		}
		wd, err := os.Getwd()
		if err != nil {
			cli.Fail("Could not read the current directory: %v", err)
			cli.OsExit(1)
		}
		projName = filepath.Base(wd)
		if err := refuseLinkedToolFiles("."); err != nil {
			cli.Fail("%v", err)
			cli.Info("This invocation has not written any files.")
			cli.OsExit(1)
			return
		}
		for _, f := range []string{".env", settings.FileName} {
			if err := projectfs.CheckRegularLeaf(".", f); err != nil {
				cli.Fail("%v", err)
				cli.Info("This invocation has not written any files.")
				cli.OsExit(1)
				return
			}
		}
		existingModule, err := existingModulePath(".")
		if err != nil {
			cli.Fail("Could not read the existing go.mod: %v", err)
			cli.OsExit(1)
			return
		}
		if existingModule != "" {
			if modulePath != "" && modulePath != existingModule {
				cli.Fail("--module=%s does not match this directory's go.mod, which declares module %s.", modulePath, existingModule)
				cli.Info("This invocation has not written any files. Drop --module, or change the module line in go.mod first.")
				cli.OsExit(1)
				return
			}
			modulePath = existingModule
		}
		if modulePath != "" && validateProjectName(projName) != nil {
			projName = path.Base(modulePath)
		}
		if err := validateProjectName(projName); err != nil {
			cli.Fail("This directory's name %q is not a valid project name: %v", projName, err)
			cli.Info("Rename the directory, or pass --module=<path> whose last element is a valid name.")
			cli.OsExit(1)
		}
		if existing := existingScaffoldFiles(noEntity); len(existing) > 0 && !force {
			cli.Fail("init . would replace files this directory already has: %s", strings.Join(existing, ", "))
			cli.Info("This invocation has not written any files.")
			cli.Info("Move them aside, or pass --force to overwrite them.")
			cli.OsExit(1)
		}
	}

	goPath, err := exec.LookPath("go")
	if err != nil {
		cli.Fail("Go is not installed or not in PATH.")
		cli.Info("Install Go from https://go.dev/dl/")
		cli.OsExit(1)
	}
	_ = goPath

	if modulePath == "" {
		modulePath = "local/" + projName
	}
	dbURL := "file:" + projName + ".db"
	if dbDriver == "postgres" {
		dbURL = "postgres://user:password@localhost:5432/" + projName + "?sslmode=disable"
	}

	cli.Printf("\n  Creating "+cli.Bold("GoFastr")+" project "+cli.Bold("%s")+"...\n\n", projName)

	//gofastr:allow(worldreadable) the project checkout holds committed source and docs; 0755 is the mode git clone or mkdir gives it
	if err := os.MkdirAll(name, 0o755); err != nil {
		cli.Fail("Failed to create directory %q: %v", name, err)
		cli.OsExit(1)
	}
	dirs := []string{"static"}
	if err := agentscmd.CheckOnboardingFiles(name); err != nil {
		cli.Fail("Cannot write AI-agent onboarding files: %v", err)
		cli.Info("This invocation has not written any scaffold files.")
		cli.OsExit(1)
		return
	}
	if !noEntity {
		dirs = append(dirs, "entities", "migrations")
	}
	for _, dir := range dirs {
		if err := projectfs.MkdirAll(name, dir, 0o755); err != nil {
			cli.Fail("Failed to create directory %q: %v", filepath.Join(name, dir), err)
			cli.OsExit(1)
		}
	}

	writeMainGo(name, projName, modulePath, noEntity, dbDriver, dbURL)

	writeScreensGo(name, projName, noEntity)

	if !noEntity {
		writeEntitiesGo(name)
		writeAuthScreensGo(name)
	}

	var envContent string
	if noEntity {
		envContent = `# GoFastr Environment Configuration

# PORT is the listen address, not only a port number. The default below is
# loopback-only, so nothing outside this machine can reach the dev server.
#
# A bare port ("8080") is also accepted, the shape platforms like Fly,
# Render, and Heroku inject, but it binds ALL interfaces, which on a laptop
# means everyone on your network can reach the app. Keep the host prefix
# locally; use the bare form only where you intend to serve publicly.
PORT=localhost:8080
`
	} else {
		envContent = fmt.Sprintf(`# GoFastr Environment Configuration
DATABASE_URL=%s

# JWT_SECRET signs session tokens. This one was generated for this checkout
# only; set your own in every deployed environment.
JWT_SECRET=%s

# PORT is the listen address, not only a port number. The default below is
# loopback-only, so nothing outside this machine can reach the dev server.
#
# A bare port ("8080") is also accepted, the shape platforms like Fly,
# Render, and Heroku inject, but it binds ALL interfaces, which on a laptop
# means everyone on your network can reach the app. Keep the host prefix
# locally; use the bare form only where you intend to serve publicly.
PORT=localhost:8080
`, dbURL, newJWTSecret())
	}
	if err := writeEnvFile(name, envContent); err != nil {
		cli.Fail("Failed to write .env: %v", err)
		cli.OsExit(1)
	}

	writeSettingsFile(name, dbDriver)

	gitignoreContent := `gen/
.gofastr/
*.db
*.db-shm
*.db-wal
.env
bin/
`
	if err := writeGitignore(name, gitignoreContent); err != nil {
		cli.Fail("Failed to write .gitignore: %v", err)
		cli.OsExit(1)
	}

	if !noEntity {
		if err := writeScaffoldFile(name, "migrations/.gitkeep", []byte("")); err != nil {
			cli.Fail("Failed to write migrations/.gitkeep: %v", err)
			cli.OsExit(1)
		}
	}

	if err := writeScaffoldFile(name, "static/.gitkeep", []byte("")); err != nil {
		cli.Fail("Failed to write static/.gitkeep: %v", err)
		cli.OsExit(1)
	}

	if err := writeScaffoldFile(name, "AGENTS.md", agentscmd.BuildAgentsMD()); err != nil {
		cli.Fail("Failed to write AGENTS.md: %v", err)
		cli.OsExit(1)
	}
	if err := agentscmd.WriteAgentDetailFiles(name); err != nil {
		cli.Fail("Failed to write .agents/ details: %v", err)
		cli.OsExit(1)
	}

	if err := agentscmd.WriteHostSkill(name, force); err != nil {
		cli.Fail("Failed to write gofastr-host skill: %v", err)
		cli.OsExit(1)
	}

	if err := writeCLAUDEmd(name); err != nil {
		cli.Fail("Failed to write CLAUDE.md: %v", err)
		cli.OsExit(1)
	}
	if err := writeDesignMD(name); err != nil {
		cli.Fail("Failed to write DESIGN.md: %v", err)
		cli.OsExit(1)
	}

	if err := refuseLinkedToolFiles(name); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}
	minimumGo, preferredGo, err := scaffoldGoRequirements(name)
	if err != nil {
		cli.Fail("Could not read the existing go.mod: %v", err)
		cli.OsExit(1)
		return
	}
	if _, err := os.Lstat(filepath.Join(name, "go.mod")); os.IsNotExist(err) {
		cmd := exec.Command("go", "mod", "init", modulePath)
		cmd.Dir = name
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			cli.Fail("Could not run 'go mod init %s': %v", modulePath, err)
			cli.Info("The other project files are written; run 'go mod init %s' by hand.", modulePath)
			cli.OsExit(1)
			return
		}
	}
	pinnedVersion := project.InstalledFrameworkVersion()
	toolchainArg := preferredGo
	if preferredGo == "go"+minimumGo {
		toolchainArg = "none"
	}
	editArgs := []string{"mod", "edit", "-go=" + minimumGo, "-toolchain=" + toolchainArg}
	if pinnedVersion != "" {
		editArgs = append(editArgs, "-require="+project.GofastrModule+"@"+pinnedVersion)
	}
	edit := exec.Command("go", editArgs...)
	edit.Dir = name
	if out, err := edit.CombinedOutput(); err != nil {
		cli.Fail("Could not set Go requirements in go.mod: %v", err)
		if msg := strings.TrimSpace(string(out)); msg != "" {
			cli.Info("%s", msg)
		}
		cli.OsExit(1)
		return
	}

	gitCmd := exec.Command("git", "init", name)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	if err := gitCmd.Run(); err != nil {
		cli.Info("Could not run 'git init': %v", err)
		cli.Info("You may need to run 'git init' manually.")
	}

	cli.Success("Created project %s in ./%s/", projName, name)
	fmt.Println()
	fmt.Printf("  %s:\n", cli.Bold("App files"))
	fmt.Println("    main.go              : Application entry point + UI host")
	fmt.Println("    screens.go           : Sample UI page (HomeScreen) served at /")
	if !noEntity {
		fmt.Println("    screens_auth.go      : Sign-in screens at /login and /register")
		fmt.Println("    entities/entities.go : Sample entity (posts) served at /api/posts")
	}
	fmt.Println("    .env                 : Environment configuration")
	fmt.Println("    gofastr.settings.yml : Project settings (process isolation)")
	fmt.Println("    .gitignore           : Git ignore rules")
	fmt.Println()
	fmt.Printf("  %s (read by AI coding tools so they reach for framework primitives instead of reinventing):\n", cli.Bold("AI-agent onboarding"))
	fmt.Println("    CLAUDE.md            : Claude Code entry point; links to AGENTS.md + skill")
	fmt.Println("    AGENTS.md            : Top-level TOC; refresh with `gofastr agents sync`")
	fmt.Println("    DESIGN.md            : App-owned product and composition direction")
	fmt.Println("    .agents/             : Auto-generated per-battery detail files linked from AGENTS.md")
	fmt.Println("    .claude/skills/      : Claude Code skill (safe to delete if you only use Cursor/Aider)")
	fmt.Println()
	printInitNextSteps(name, noEntity)
	cli.Info("Also try:")
	cli.Info("    gofastr docs          : Browse/search framework docs (embedded in the binary)")
	cli.Info("    gofastr theme init    : Scaffold a typed theme.go")
	cli.Info("    gofastr build         : Build production binary")
	fmt.Println()
	if pinnedVersion != "" {
		cli.Printf("  GoFastr is pinned to %s, matching this CLI. `go mod tidy` will\n", pinnedVersion)
		fmt.Println("  resolve that release. Use `gofastr upgrade` when you want to move.")
	} else {
		fmt.Println("  This development CLI has no release version to pin. Before `go mod tidy`, run:")
		fmt.Printf("    go get %s@vX.Y.Z\n", project.GofastrModule)
		fmt.Println("  Release-installed CLIs pin their matching framework automatically.")
	}
	fmt.Println("  Only use a local `replace` directive while hacking on GoFastr itself.")
	fmt.Println()
}

func refuseLinkedToolFiles(dir string) error {
	for _, rel := range []string{"go.mod", ".git"} {
		if err := projectfs.Check(dir, rel); err != nil {
			return err
		}
	}
	return nil
}

var initValuedFlags = map[string]bool{"--module": true, "--db": true}

func splitInitArgs(args []string) (name string, flags []string, err error) {
	named := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if len(arg) < 2 || arg[0] != '-' {
			if named {
				return "", nil, fmt.Errorf("unexpected argument %q: init takes one project name", arg)
			}
			name, named = arg, true
			continue
		}
		if initValuedFlags[arg] {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return "", nil, fmt.Errorf("%s needs a value (%s=<value> or %s <value>)", arg, arg, arg)
			}
			i++
			arg += "=" + args[i]
		}
		flags = append(flags, arg)
	}
	return name, flags, nil
}

func existingModulePath(dir string) (string, error) {
	path := filepath.Join(dir, "go.mod")
	data, err := projectfs.ReadFile(dir, "go.mod")
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	mod, err := modfile.ParseLax(path, data, nil)
	if err != nil {
		return "", err
	}
	if mod.Module == nil || mod.Module.Mod.Path == "" {
		return "", fmt.Errorf("%s has no module line", path)
	}
	return mod.Module.Mod.Path, nil
}

func scaffoldGoRequirements(dir string) (minimum, preferred string, err error) {
	minimum, preferred = frameworkGoDirective, frameworkToolchain
	path := filepath.Join(dir, "go.mod")
	data, err := projectfs.ReadFile(dir, "go.mod")
	if errors.Is(err, fs.ErrNotExist) {
		return minimum, preferred, nil
	}
	if err != nil {
		return "", "", err
	}
	mod, err := modfile.Parse(path, data, nil)
	if err != nil {
		return "", "", err
	}
	if mod.Go != nil && goversion.Compare("go"+mod.Go.Version, "go"+minimum) > 0 {
		minimum = mod.Go.Version
	}
	if mod.Toolchain != nil {
		if mod.Toolchain.Name == "default" {
			return minimum, "default", nil
		}
		if goversion.Compare(mod.Toolchain.Name, preferred) > 0 {
			preferred = mod.Toolchain.Name
		}
	}
	if goversion.Compare("go"+minimum, preferred) > 0 {
		preferred = "go" + minimum
	}
	return minimum, preferred, nil
}

func printInitNextSteps(name string, noEntity bool) {
	fmt.Printf("  %s:\n", cli.Bold("Next steps"))
	cli.Printf("    cd %s\n", name)
	fmt.Println("    go mod tidy          : Resolve dependencies")
	fmt.Println("    gofastr dev          : Start development server with hot-reload")
	if !noEntity {
		fmt.Println("    Note: /api/posts needs a session by default. Register at /register,")
		fmt.Println("    sign in at /login (battery/auth is already wired), or add Public: true")
		fmt.Println("    (see entities/entities.go) for anonymous access.")
	}
	fmt.Println()
}

func printInitHelp() {
	fmt.Println(`gofastr init: scaffold a new GoFastr project

Usage:
  gofastr init <name> [flags]
  gofastr init . [flags]
  gofastr init . --reinit [--force]

Flags:
  --module=<path>  Go module path (default: local/<name>; for init .,
                   the existing go.mod's module, else local/<directory name>)
  --db=<driver>    sqlite (default) or postgres (aliases: sqlite3, postgresql)
  --no-entity      Omit the sample entity and database wiring
  --reinit         Refresh AI onboarding files in an existing project
  --force          With --reinit, overwrite customized onboarding files;
                   with init ., overwrite the files the project already has

Flags go before or after the name; --module and --db take their value
as --module=<path> or --module <path>.

The generated go.mod pins the GoFastr release matching this CLI and
carries GoFastr's go line, not the local Go version, plus a toolchain
line only when GoFastr prefers a newer release than that minimum.
Local development builds cannot infer a release and print the exact
go get command that must be run before go mod tidy.`)
}

const (
	frameworkGoDirective = "1.27.4"
	frameworkToolchain   = "go1.27.4"
)

func writeMainGo(name, appName, modulePath string, noEntity bool, dbDriver, dbURL string) {
	var content string
	if noEntity {
		content = fmt.Sprintf(mainGoTemplate, modulePath, appName)
	} else {
		driverImport := `_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"`
		migrateDialect := "migrate.DialectSQLite"
		sqlDriver := "sqlite3"
		if dbDriver == "postgres" {
			driverImport = `_ "github.com/lib/pq"`
			migrateDialect = "migrate.DialectPostgres"
			sqlDriver = "postgres"
		}
		dsnFallback := dbURL
		if dsnredact.HasSecret(dbURL) {
			dsnFallback = ""
		}
		content = fmt.Sprintf(mainEntitiesGoTemplate, modulePath, appName, driverImport, sqlDriver, dsnFallback, migrateDialect)
	}
	if err := writeScaffoldFile(name, "main.go", []byte(content+project.GetEnvSource)); err != nil {
		cli.Fail("Failed to write main.go: %v", err)
		cli.OsExit(1)
	}
}

func envTrackedByGit(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".env")); err != nil {
		return false
	}
	cmd := exec.Command("git", "-C", dir, "ls-files", "--error-unmatch", "--", ".env")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	return cmd.Run() == nil
}

func writeEnvFile(dir, content string) error {
	return projectfs.WriteOwnerOnly(dir, ".env", []byte(content))
}

func writeScaffoldFile(dir, name string, data []byte) error {
	return projectfs.WriteFile(dir, name, data, 0o644)
}

func existingScaffoldFiles(noEntity bool) []string {
	files := []string{"main.go", "screens.go", ".env", settings.FileName, "AGENTS.md", "CLAUDE.md", "DESIGN.md",
		filepath.Join(".claude", "skills", "gofastr-host", "SKILL.md")}
	if !noEntity {
		files = append(files, "screens_auth.go", filepath.Join("entities", "entities.go"))
	}
	var existing []string
	for _, f := range files {
		if _, err := os.Lstat(f); err == nil {
			existing = append(existing, f)
		}
	}
	return existing
}

func writeGitignore(dir, content string) error {
	existing, err := projectfs.ReadFile(dir, ".gitignore")
	if errors.Is(err, fs.ErrNotExist) {
		return writeScaffoldFile(dir, ".gitignore", []byte(content))
	}
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, line := range strings.Split(string(existing), "\n") {
		have[strings.TrimSpace(line)] = true
	}
	var add strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(content), "\n") {
		if !have[line] {
			add.WriteString(line + "\n")
		}
	}
	if add.Len() == 0 {
		return nil
	}
	merged := string(existing)
	if merged != "" && !strings.HasSuffix(merged, "\n") {
		merged += "\n"
	}
	return writeScaffoldFile(dir, ".gitignore", []byte(merged+add.String()))
}

func writeSettingsFile(name, _ string) {
	content := `version: 1
isolation:
  enabled: true
  mode: worktree
  port:
    offset: 1000
    range: 1000
    scan: 20
  services:
  env:
`
	if err := projectfs.WriteOwnerOnly(name, settings.FileName, []byte(content)); err != nil {
		cli.Fail("Failed to write %s: %v", settings.FileName, err)
		cli.OsExit(1)
	}
}

func writeScreensGo(name, appName string, noEntity bool) {
	var entityHint, actions string
	if !noEntity {
		actions = `
				Actions: ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
					ui.LinkButton(ui.LinkButtonConfig{Label: "Sign in", Href: "/login"}),
					ui.LinkButton(ui.LinkButtonConfig{Label: "Create account", Href: "/register", Variant: ui.ButtonSecondary}),
				),`
		entityHint = " The sample posts entity lives in entities/entities.go and serves JSON at /api/posts once you sign in."
	}
	content := fmt.Sprintf(screensGoTemplate, appName, entityHint, actions)
	if err := writeScaffoldFile(name, "screens.go", []byte(content)); err != nil {
		cli.Fail("Failed to write screens.go: %v", err)
		cli.OsExit(1)
	}
}

func newJWTSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		cli.Fail("Failed to generate JWT_SECRET: %v", err)
		cli.OsExit(1)
	}
	return hex.EncodeToString(b)
}

func writeAuthScreensGo(name string) {
	content := authScreensGoTemplate
	if err := writeScaffoldFile(name, "screens_auth.go", []byte(content)); err != nil {
		cli.Fail("Failed to write screens_auth.go: %v", err)
		cli.OsExit(1)
	}
}

func writeEntitiesGo(name string) {
	content := entitiesGoTemplate
	if err := writeScaffoldFile(name, "entities/entities.go", []byte(content)); err != nil {
		cli.Fail("Failed to write entities/entities.go: %v", err)
		cli.OsExit(1)
	}
}

func validateProjectName(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if name == "." {
		return nil
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return fmt.Errorf("character %q is not allowed", r)
		}
	}
	if name[0] == '-' || name[0] == '_' {
		return fmt.Errorf("name must start with a letter or digit")
	}
	if windowsDeviceName(name) {
		return fmt.Errorf("%q is a reserved device name on Windows", name)
	}
	return nil
}

func windowsDeviceName(name string) bool {
	switch name {
	case "con", "prn", "aux", "nul":
		return true
	}
	return len(name) == 4 && (name[:3] == "com" || name[:3] == "lpt") && name[3] >= '0' && name[3] <= '9'
}

func writeCLAUDEmd(dir string) error {
	return writeScaffoldFile(dir, "CLAUDE.md", claudeMDContent())
}

func claudeMDContent() []byte {
	const content = `# CLAUDE.md: GoFastr host project

This project uses the [GoFastr](https://github.com/DonaldMurillo/gofastr) framework.

## Start here

- **[AGENTS.md](AGENTS.md)**: TOC of every framework primitive with trigger
  phrases. When your task matches a row, open the linked detail file under
  ` + "`" + `.agents/` + "`" + ` for the full shape/import/don't-reinvent breakdown.
- **[DESIGN.md](DESIGN.md)**: app-owned product direction. Complete its user
  task, density, hierarchy, composition, and mobile decisions before choosing
  UI components.
- **[.claude/skills/gofastr-host/SKILL.md](.claude/skills/gofastr-host/SKILL.md)**
  : Auto-loaded skill that encodes the "reach for the battery first" rule and
  the import paths you need.

## Run the app while you work

` + agentscmd.DevLoopGuidance + `
## Framework docs (embedded in the CLI)

The ` + "`" + `gofastr` + "`" + ` binary ships with the full framework reference docs
built in: no internet needed, always matches your installed version.

- ` + "`" + `gofastr docs` + "`" + `               : list every topic
- ` + "`" + `gofastr docs <topic>` + "`" + `       : read a topic's full markdown
- ` + "`" + `gofastr docs --grep <term>` + "`" + ` : search across every topic
- ` + "`" + `gofastr docs ui-composition-recipes` + "`" + `: choose a framework-native page composition

## Refreshing after a framework upgrade

` + "`" + `gofastr agents sync` + "`" + `: refreshes AGENTS.md and .agents/ detail files.

## Adding to this project

- **Add an entity**: add another ` + "`" + `app.Entity("<table>", entity.EntityConfig{...})` + "`" + `
  call to ` + "`" + `RegisterAll` + "`" + ` in ` + "`" + `entities/entities.go` + "`" + ` (a project made
  with ` + "`" + `--no-entity` + "`" + ` calls ` + "`" + `fwApp.Entity` + "`" + ` from ` + "`" + `main.go` + "`" + `). Auto-migrate
  creates the table at boot; add a versioned migration to
  ` + "`" + `RegisterMigrations` + "`" + ` before a deploy. See
  ` + "`" + `gofastr docs entity-declarations` + "`" + `.
- **Add a screen**: add a type with a ` + "`" + `Render() render.HTML` + "`" + ` method
  next to ` + "`" + `HomeScreen` + "`" + ` in ` + "`" + `screens.go` + "`" + ` (or its own file) and register it
  in ` + "`" + `main.go` + "`" + ` with ` + "`" + `site.Register("/about", &AboutScreen{}, nil)` + "`" + `.
- The app generator scaffolds (` + "`" + `gofastr docs app-spec` + "`" + `) extend apps made by
  ` + "`" + `gofastr generate app <spec.yml>` + "`" + `, not this layout.
- **UI: prefer the typed ` + "`" + `core-ui/html` + "`" + ` config structs**
  (` + "`" + `html.Div(html.DivConfig{...})` + "`" + `, ` + "`" + `html.Heading(...)` + "`" + `, …) over raw
  ` + "`" + `render.Tag(...)` + "`" + ` calls. ` + "`" + `render.Tag` + "`" + ` is the escape hatch for
  cases the typed vocab doesn't cover, not the default. See
  ` + "`" + `gofastr docs ui-getting-started` + "`" + `.

## Quick reference

- ` + "`" + `gofastr dev` + "`" + `         : dev server with hot-reload
- ` + "`" + `gofastr build` + "`" + `       : production binary
- ` + "`" + `gofastr docs` + "`" + `         : browse/search framework docs
- ` + "`" + `gofastr agents sync` + "`" + ` : refresh AI-agent onboarding files
- ` + "`" + `gofastr theme init` + "`" + `  : scaffold a typed theme.go
- ` + "`" + `gofastr docs entity-declarations` + "`" + `: every entity option
`
	return []byte(content)
}

func writeDesignMD(dir string) error {
	return writeScaffoldFile(dir, "DESIGN.md", designMDContent())
}

func designMDContent() []byte {
	return []byte(`# Design direction

Complete this file before selecting UI components. Keep it product-specific:
describe what this app's users need, not a generic preferred aesthetic.

## Product intent

- Primary user:
- Primary task on each route:
- Decision or action that must be obvious first:
- Information that is secondary or archival:

## Visual direction

- Personality (three concrete adjectives):
- Density (compact, balanced, or spacious) and why:
- Dominant element on each route:
- Typography posture (display, body, and data):
- Surface posture (flat, bordered, elevated, or mixed):

## Composition

Read ` + "`" + `gofastr docs ui-composition-recipes` + "`" + `, then record:

- Chosen recipe and named framework primitives for each route:
- Composition model for each route:
- How section widths and visual weight vary:
- Wide-screen use on detail routes; which bounded modules pair into columns:
- What groups without a Card:
- Content shown once rather than repeated across Banner/summary/header:
- Where actions stay natural-width (component Actions slot or Cluster):
- Two familiar patterns this product should avoid:

## Mobile contract

- Primary mobile action:
- First-viewport priority order:
- Opening summary is one or two sentences; full narrative moves to:
- Concise mobile header identity (a short wordmark when the name is long):
- What moves later, collapses, or becomes a separate view:
- Long labels, metadata, and edge-aligned row behavior:

## Verification

- [ ] Rendered at about 390px and 1440px.
- [ ] Checked in light and dark schemes.
- [ ] One element is clearly dominant on every route.
- [ ] The next decision/action appears in the first useful viewport.
- [ ] Repeated status/summary content and stretched standalone CTAs were removed.
- [ ] Cards, pills, elevation, and equal-width grids are used only when the content warrants them.
- [ ] No narrow desktop detail column leaves an accidental empty half-canvas.
- [ ] Mobile is reprioritized for its user task, not merely stacked.
- [ ] The three weakest visible decisions were identified and revised.

## Framework boundary

Applications compose ` + "`" + `framework/ui` + "`" + `, ` + "`" + `core-ui/app` + "`" + `, and ` + "`" + `framework/headless` + "`" + `.
They do not ship bespoke CSS or recreate structural components. When the design
system cannot express a required treatment, record the gap and add the reusable
capability upstream.
`)
}

func runReinit(dir string, force bool) {
	cli.Printf("\n  "+cli.Bold("Refreshing")+" AI onboarding files in "+cli.Bold("%s")+"\n\n", dir)

	if err := agentscmd.WriteAgentDetailFiles(dir); err != nil {
		cli.Fail("Failed to refresh .agents/ details: %v", err)
		cli.OsExit(1)
	}
	cli.Info("  ✓ .agents/ detail files refreshed")

	if err := agentscmd.WriteHostSkill(dir, force); err != nil {
		cli.Fail("Failed to refresh gofastr-host skill: %v", err)
		cli.OsExit(1)
	}
	cli.Info("  ✓ .claude/skills/gofastr-host/SKILL.md refreshed")

	existing, err := agentscmd.ReadProjectFile(dir, "AGENTS.md")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		cli.Fail("Cannot read AGENTS.md: %v", err)
		cli.OsExit(1)
		return
	}
	if err != nil {
		if err := agentscmd.WriteProjectFile(dir, "AGENTS.md", agentscmd.BuildAgentsMD()); err != nil {
			cli.Fail("Failed to write AGENTS.md: %v", err)
			cli.OsExit(1)
		}
		cli.Info("  ✓ AGENTS.md created")
	} else {
		refreshed, changed, err := agentscmd.RefreshAgentsMD(existing)
		switch {
		case err != nil && force:
			if err := agentscmd.WriteProjectFile(dir, "AGENTS.md", agentscmd.BuildAgentsMD()); err != nil {
				cli.Fail("Failed to write AGENTS.md: %v", err)
				cli.OsExit(1)
			}
			cli.Warn("  ⚠ AGENTS.md overwritten (--force): its markers were missing, so your changes were replaced")
		case err != nil:
			cli.Fail("AGENTS.md sync failed: %v", err)
			cli.Info("  The file may have been edited without preserving the auto-generated markers.")
			cli.Info("  Run `gofastr init %s --reinit --force` to overwrite it, or fix the markers by hand.", dir)
			cli.OsExit(1)
		case changed:
			if err := agentscmd.WriteProjectFile(dir, "AGENTS.md", refreshed); err != nil {
				cli.Fail("Failed to write AGENTS.md: %v", err)
				cli.OsExit(1)
			}
			cli.Info("  ✓ AGENTS.md synced (auto section updated, your changes preserved)")
		default:
			cli.Info("  ✓ AGENTS.md already up to date")
		}
	}

	existingClaude, err := agentscmd.ReadProjectFile(dir, "CLAUDE.md")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		cli.Fail("Cannot read CLAUDE.md: %v", err)
		cli.OsExit(1)
		return
	}
	if err != nil {
		if err := agentscmd.WriteProjectFile(dir, "CLAUDE.md", claudeMDContent()); err != nil {
			cli.Fail("Failed to write CLAUDE.md: %v", err)
			cli.OsExit(1)
		}
		cli.Info("  ✓ CLAUDE.md created")
	} else {
		generated := claudeMDContent()
		legacyGenerated := bytes.ReplaceAll(generated, []byte(agentscmd.AgentsDetailDir+"/"), []byte(agentscmd.LegacyAgentsDetailDir+"/"))
		if bytes.Equal(agentscmd.NormalizeMD(existingClaude), agentscmd.NormalizeMD(generated)) ||
			bytes.Equal(agentscmd.NormalizeMD(existingClaude), agentscmd.NormalizeMD(legacyGenerated)) {
			if err := agentscmd.WriteProjectFile(dir, "CLAUDE.md", generated); err != nil {
				cli.Fail("Failed to write CLAUDE.md: %v", err)
				cli.OsExit(1)
			}
			cli.Info("  ✓ CLAUDE.md refreshed (unchanged from generated)")
		} else if force {
			if err := agentscmd.WriteProjectFile(dir, "CLAUDE.md", generated); err != nil {
				cli.Fail("Failed to write CLAUDE.md: %v", err)
				cli.OsExit(1)
			}
			cli.Warn("  ⚠ CLAUDE.md overwritten (--force): your customizations were replaced")
		} else {
			cli.Warn("  ⚠ CLAUDE.md has been modified: not overwriting")
			cli.Info("     To overwrite: gofastr init --reinit --force")
			cli.Info("     To keep your version: no action needed")
		}
	}

	designPath := filepath.Join(dir, "DESIGN.md")
	if _, err := os.Stat(designPath); os.IsNotExist(err) {
		if err := agentscmd.WriteProjectFile(dir, "DESIGN.md", designMDContent()); err != nil {
			cli.Fail("Failed to write DESIGN.md: %v", err)
			cli.OsExit(1)
		}
		cli.Info("  ✓ DESIGN.md created")
	} else if err != nil {
		cli.Fail("Failed to inspect DESIGN.md: %v", err)
		cli.OsExit(1)
	} else {
		cli.Info("  ✓ DESIGN.md preserved (app-owned)")
	}

	fmt.Println()
	cli.Success("Reinit complete. All AI onboarding files refreshed.")
}
