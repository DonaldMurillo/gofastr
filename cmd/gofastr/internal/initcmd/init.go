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
	"github.com/DonaldMurillo/gofastr/core/settings"
	"github.com/DonaldMurillo/gofastr/internal/atomicfile"
	"github.com/DonaldMurillo/gofastr/internal/dsnredact"
	"github.com/DonaldMurillo/gofastr/internal/fileperm"
	"golang.org/x/mod/modfile"
)

func RunInit(args []string) {
	if cli.HasHelpFlag(args) {
		printInitHelp()
		return
	}
	if len(args) == 0 {
		cli.Fail("Project name is required.")
		cli.Info("Usage: gofastr init <project-name>")
		cli.Info("Example: gofastr init myapp")
		cli.OsExit(1)
	}

	name := args[0]

	// Validate project name
	if err := validateProjectName(name); err != nil {
		cli.Fail("Invalid project name %q: %v", name, err)
		cli.Info("Use lowercase letters, digits, and hyphens. Example: my-blog-app")
		cli.OsExit(1)
	}

	modulePath := ""
	dbDriver := "sqlite3"
	noEntity := false
	reinit := false
	force := false

	// Allow overriding via flags
	for _, arg := range args[1:] {
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
		case arg == "--no-entity":
			noEntity = true
		case strings.HasPrefix(arg, "--db="):
			// Accept the docs-canonical spellings (sqlite, postgres) plus the
			// wire/alias spellings (sqlite3, postgresql) the framework already
			// honors, and map them to the real driver name. Anything else used
			// to fall through and silently scaffold SQLite (dbDriver set, dbURL
			// left as a SQLite file): a broken app that looked configured.
			// canonicalDBDriver is the alias table migrate --db= shares.
			switch driver := project.CanonicalDBDriver(strings.TrimPrefix(arg, "--db=")); driver {
			case "sqlite3", "postgres":
				dbDriver = driver
			default:
				choice := strings.TrimPrefix(arg, "--db=")
				cli.Fail("--db=%q is not a recognized database driver. Accepted values: sqlite, postgres (aliases: sqlite3, postgresql).", choice)
				cli.OsExit(1)
			}
		default:
			// A misspelled flag used to be skipped, so `--noentty`
			// scaffolded the default app and exited 0.
			cli.Fail("Unknown flag %q.", arg)
			cli.Info("Run 'gofastr init --help' for the accepted flags.")
			cli.OsExit(2)
		}
	}

	// --reinit: refresh AI onboarding files only (no Go code, no git). It
	// refreshes a project that exists, so it runs before the guard that
	// refuses one.
	if reinit {
		runReinit(name, force)
		return
	}

	// projName names the app, its module and its database; name is the
	// directory the files go to. They differ only for init-in-place.
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
			// init . rewrites .env with a fresh JWT_SECRET. The .gitignore entry
			// the scaffold adds does not untrack a file git already tracks, so
			// the next commit would publish the key. Refuse before writing.
			cli.Fail(".env is tracked by git; init would write a signing key into it.")
			cli.Info("Untrack it first: git rm --cached .env")
			cli.OsExit(1)
		}
		// The project is named after its directory. "." itself named the
		// module local/., which go mod init refuses, after every other
		// file was written.
		wd, err := os.Getwd()
		if err != nil {
			cli.Fail("Could not read the current directory: %v", err)
			cli.OsExit(1)
		}
		projName = filepath.Base(wd)
		// A go.mod already here names the module every generated import
		// must use; go mod init cannot replace it, so a local/<dir>
		// default would leave main.go importing a module that does not
		// exist.
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
		// A repeat `init <name>` writes nothing; init . holds the same
		// line over the files a project already has, unless asked.
		if existing := existingScaffoldFiles(noEntity); len(existing) > 0 && !force {
			cli.Fail("init . would replace files this directory already has: %s", strings.Join(existing, ", "))
			cli.Info("This invocation has not written any files.")
			cli.Info("Move them aside, or pass --force to overwrite them.")
			cli.OsExit(1)
		}
	}

	// Check for 'go' in PATH
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

	fmt.Printf("\n  Creating %s project %s...\n\n", cli.Bold("GoFastr"), cli.Bold(projName))

	// Create directory structure
	dirs := []string{
		name,
		filepath.Join(name, "static"),
	}
	if !noEntity {
		dirs = append(dirs,
			filepath.Join(name, "entities"),
			filepath.Join(name, "migrations"),
		)
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			cli.Fail("Failed to create directory %q: %v", dir, err)
			cli.OsExit(1)
		}
	}

	// Write main.go
	writeMainGo(name, projName, modulePath, noEntity, dbDriver, dbURL)

	// Write screens.go (owned package main: HomeScreen beside main.go).
	writeScreensGo(name, projName, noEntity)

	// Write entities/entities.go and the sign-in screens (only when
	// --no-entity is not set: auth needs the database).
	if !noEntity {
		writeEntitiesGo(name)
		writeAuthScreensGo(name)
	}

	// Write .env
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
	// 0600, not 0644: this file already carries DATABASE_URL (which may
	// embed a password) and is where the operator is told to put
	// GOFASTR_SECRET, the session-signing key. Scaffolding it
	// world-readable hands every local account the app's secrets.
	//
	// os.WriteFile applies its mode only when CREATING the file; on an
	// existing one it truncates and leaves the mode alone. `gofastr init .`
	// runs in a directory that may already hold a 0644 .env, so the mode is
	// set explicitly, and set BEFORE the write, so no secret is ever on disk
	// while the file is world-readable.
	if err := writeEnvFile(filepath.Join(name, ".env"), envContent); err != nil {
		cli.Fail("Failed to write .env: %v", err)
		cli.OsExit(1)
	}

	writeSettingsFile(name, dbDriver)

	// Write .gitignore. gen/ holds generated Go (regenerated, not committed);
	// .gofastr/ holds local runtime state (worktree DB isolation, harness).
	gitignoreContent := `gen/
.gofastr/
*.db
*.db-shm
*.db-wal
.env
bin/
`
	if err := writeGitignore(filepath.Join(name, ".gitignore"), gitignoreContent); err != nil {
		cli.Fail("Failed to write .gitignore: %v", err)
		cli.OsExit(1)
	}

	// Write migrations/.gitkeep (only with entities)
	if !noEntity {
		if err := writeScaffoldFile(filepath.Join(name, "migrations", ".gitkeep"), []byte("")); err != nil {
			cli.Fail("Failed to write migrations/.gitkeep: %v", err)
			cli.OsExit(1)
		}
	}

	// Write static/.gitkeep
	if err := writeScaffoldFile(filepath.Join(name, "static", ".gitkeep"), []byte("")); err != nil {
		cli.Fail("Failed to write static/.gitkeep: %v", err)
		cli.OsExit(1)
	}

	// Write AGENTS.md (thin TOC) + .agents/ detail files so AI agents
	// working on this project find the framework primitives instead
	// of reinventing them. Refresh later with `gofastr agents sync`.
	if err := writeScaffoldFile(filepath.Join(name, "AGENTS.md"), agentscmd.BuildAgentsMD()); err != nil {
		cli.Fail("Failed to write AGENTS.md: %v", err)
		cli.OsExit(1)
	}
	if err := agentscmd.WriteAgentDetailFiles(name); err != nil {
		cli.Fail("Failed to write .agents/ details: %v", err)
		cli.OsExit(1)
	}

	// Drop the gofastr-host Claude Code skill so AI agents working on
	// this project auto-load the framework's host-app guidance at task
	// start. Refresh manually with `gofastr agents skill`.
	if err := agentscmd.WriteHostSkill(name); err != nil {
		cli.Fail("Failed to write gofastr-host skill: %v", err)
		cli.OsExit(1)
	}

	// Write CLAUDE.md: thin entry point for Claude Code that points
	// agents at the richer AGENTS.md and the gofastr-host skill.
	if err := writeCLAUDEmd(name); err != nil {
		cli.Fail("Failed to write CLAUDE.md: %v", err)
		cli.OsExit(1)
	}
	if err := writeDesignMD(name); err != nil {
		cli.Fail("Failed to write DESIGN.md: %v", err)
		cli.OsExit(1)
	}

	// Run go mod init to generate a proper go.mod
	minimumGo, preferredGo, err := scaffoldGoRequirements(name)
	if err != nil {
		cli.Fail("Could not read the existing go.mod: %v", err)
		cli.OsExit(1)
		return
	}
	// An existing go.mod (init .) already declares modulePath; go mod
	// init would only refuse to overwrite it.
	if _, err := os.Stat(filepath.Join(name, "go.mod")); os.IsNotExist(err) {
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
	// go mod init wrote the local toolchain's go line; state the
	// framework's instead, and pin the release matching this CLI. A
	// release build still fails loudly when there is no go.mod to pin.
	pinnedVersion := project.InstalledFrameworkVersion()
	// Set all directives in one edit: after the go line changes, a second
	// go invocation could switch to the minimum toolchain before seeing the
	// preferred patch release. Do this for development CLIs too.
	// A toolchain line that repeats the go line is one go mod tidy
	// removes, so it is never written.
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

	// Run git init so the project starts with a clean repo.
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
		fmt.Printf("  GoFastr is pinned to %s, matching this CLI. `go mod tidy` will\n", pinnedVersion)
		fmt.Println("  resolve that release. Use `gofastr upgrade` when you want to move.")
	} else {
		fmt.Println("  This development CLI has no release version to pin. Before `go mod tidy`, run:")
		fmt.Printf("    go get %s@vX.Y.Z\n", project.GofastrModule)
		fmt.Println("  Release-installed CLIs pin their matching framework automatically.")
	}
	fmt.Println("  Only use a local `replace` directive while hacking on GoFastr itself.")
	fmt.Println()
}

// existingModulePath returns the module path of dir/go.mod, or "" when
// there is no go.mod.
func existingModulePath(dir string) (string, error) {
	path := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
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

// scaffoldGoRequirements reads an existing module before go mod init runs.
// Fresh modules use the release defaults; init . must not lower a project's
// existing Go or toolchain requirement.
func scaffoldGoRequirements(dir string) (minimum, preferred string, err error) {
	minimum, preferred = frameworkGoDirective, frameworkToolchain
	path := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
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

// printInitNextSteps prints the "Next steps" block at the end of
// `gofastr init`. Kept as its own function so the guidance printed there
// (secure-by-default CRUD and its escape hatches) is testable without
// scaffolding a whole project.
func printInitNextSteps(name string, noEntity bool) {
	fmt.Printf("  %s:\n", cli.Bold("Next steps"))
	fmt.Printf("    cd %s\n", name)
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

The generated go.mod pins the GoFastr release matching this CLI and
carries GoFastr's go line, not the local Go version, plus a toolchain
line only when GoFastr prefers a newer release than that minimum.
Local development builds cannot infer a release and print the exact
go get command that must be run before go mod tidy.`)
}

// frameworkGoDirective and frameworkToolchain are the go and toolchain
// lines of GoFastr's own go.mod (TestFrameworkGoMatchesGoMod holds them
// to it). go mod init writes the local toolchain's version instead, which
// can be older than the framework needs. The floor is 1.27.3 because
// earlier 1.27 releases build crypto/x509 with GO-2026-6809; a toolchain
// equal to the go line is written as no toolchain line, as go mod tidy
// leaves it.
const (
	frameworkGoDirective = "1.27.3"
	frameworkToolchain   = "go1.27.3"
)

// writeMainGo generates the application entry point.
func writeMainGo(name, appName, modulePath string, noEntity bool, dbDriver, dbURL string) {
	var content string
	if noEntity {
		content = fmt.Sprintf(`package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/docs/mcptools"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

func main() {
	runtimeIsolation, err := isolation.Resolve(".")
	if err != nil {
		log.Fatal(err)
	}

	fwApp := framework.NewApp(
		// WithConfig replaces the whole AppConfig, so it goes FIRST: a
		// granular option (framework.WithPublicOpenAPI(), WithAPIPrefix, …)
		// added below this line takes effect; above it, WithConfig would
		// discard it (the framework warns at boot if that happens).
		framework.WithConfig(framework.AppConfig{Name: "%[2]s"}),
		// The framework's own docs as MCP tools (framework_docs_list /
		// _get / _search), so the coding agent on /mcp reads them from the
		// running app under `+"`gofastr dev`"+`. Drop this line to keep the docs
		// corpus (about 2 MB) out of the binary.
		framework.WithMCPTools(mcptools.Register),
	)

	// Wire the UI layer: site app + home screen + host.
	site := app.NewApp("%[2]s").WithTheme(uitheme.Default())
	site.Register("/", &HomeScreen{}, nil)

	fwApp.Mount(uihost.New(site))

	addr, err := runtimeIsolation.Addr(getEnv("PORT", "localhost:8080"))
	if err != nil {
		log.Fatal(err)
	}
	// Banner fires via OnReady: only after migrations, hooks, and the
	// port bind all succeeded.
	fwApp.OnReady(func(boundAddr string) {
		fmt.Printf("  %%s Server running at http://%%s\n", "✓", boundAddr)
	})
	if err := fwApp.Start(addr); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
`, modulePath, appName)
	} else {
		driverImport := `_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"`
		migrateDialect := "migrate.DialectSQLite"
		sqlDriver := "sqlite3"
		if dbDriver == "postgres" {
			driverImport = `_ "github.com/lib/pq"`
			migrateDialect = "migrate.DialectPostgres"
			sqlDriver = "postgres"
		}
		// Secrets never land in committed source (the app spec
		// generator's own contract, TestAppSpecNeverInlinesSecrets).
		// dsnredact.HasSecret is the same classifier the generate path
		// uses: a credentialed DSN — the postgres placeholder included —
		// has its only home in the 0600, gitignored .env this scaffold
		// writes, and main.go carries a bare env read with no secret
		// fallback. SQLite file DSNs stay inline: a local file path is
		// not a credential.
		dsnFallback := dbURL
		if dsnredact.HasSecret(dbURL) {
			dsnFallback = ""
		}
		content = fmt.Sprintf(`package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/dotenv"
	"github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/dev"
	"github.com/DonaldMurillo/gofastr/framework/docs/mcptools"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	uitheme "github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	%[3]s

	"%[1]s/entities"
)

func main() {
	// Load .env BEFORE any getEnv read. framework.NewApp also loads it, but
	// DATABASE_URL is consumed here, above NewApp, so without this the
	// scaffolded .env was half-live: PORT applied, DATABASE_URL silently did
	// not, and the app opened a different database than the file said.
	//
	// framework.DefaultDotEnvPaths(), not a hardcoded list: dotenv.Apply never
	// overwrites an existing variable, so loading a SHORTER list here would pin
	// .env's value and NewApp's higher-precedence .env.<APP_ENV> could no
	// longer win.
	_ = dotenv.LoadAndApply(framework.DefaultDotEnvPaths()...)

	runtimeIsolation, err := isolation.Resolve(".")
	if err != nil {
		log.Fatal(err)
	}
	dsn := getEnv("DATABASE_URL", "%[5]s")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set: the scaffolded .env (0600, gitignored) carries it; copy it next to the binary or export the variable")
	}
	driver, resolvedDSN, err := runtimeIsolation.Database("%[4]s", dsn)
	if err != nil {
		log.Fatal(err)
	}
	db, err := sql.Open(driver, resolvedDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	fwApp := framework.NewApp(
		// WithConfig replaces the whole AppConfig, so it goes FIRST: a
		// granular option (framework.WithPublicOpenAPI(), WithAPIPrefix, …)
		// added below this line takes effect; above it, WithConfig would
		// discard it (the framework warns at boot if that happens).
		framework.WithConfig(framework.AppConfig{Name: "%[2]s"}),
		framework.WithDB(db),
		// Entity CRUD lives under /api (/api/posts), so a screen can own
		// /posts without colliding with the JSON route.
		framework.WithAPIPrefix("/api"),
		// Mounts /mcp (POST JSON-RPC + GET SSE). Every entity with
		// Exposure.MCP gets list/get/create/update/delete tools, and each tool
		// call runs through this app's router, so an agent gets exactly the
		// permissions the calling session has, and an anonymous call answers
		// with the same error the REST route would. That parity is the point:
		// there is no second authorization path to keep in sync.
		//
		// Turn this off before exposing the app to untrusted callers if you
		// have not reviewed which entities set Exposure.MCP.
		framework.WithMCP(),
		// The framework's own docs as MCP tools (framework_docs_list /
		// _get / _search), so the coding agent on /mcp reads them from the
		// running app. Drop this line to keep the docs corpus (about 2 MB)
		// out of the binary.
		framework.WithMCPTools(mcptools.Register),
	)

	// Register entities from the entities package.
	entities.RegisterAll(fwApp)

	// Sign-in: POST /auth/register, /auth/login and /auth/logout, with
	// users and sessions in this database. SessionMiddleware resolves the
	// session cookie on every request, so /api/posts sees the signed-in
	// user. Dev mode (plain session cookie, relaxed login limiter) follows
	// the dev loop's own signal: GOFASTR_DEV, which `+"`gofastr dev`"+` sets and
	// GOFASTR_ENV=production overrides. It is never read from .env, so a
	// copied .env cannot downgrade a deployment; outside the dev loop auth
	// issues Secure cookies and refuses to boot without JWT_SECRET.
	authMgr := auth.New(auth.AuthConfig{
		DevMode:      dev.Enabled(),
		JWTSecret:    os.Getenv("JWT_SECRET"),
		UserStore:    auth.NewEntityUserStore(db, "auth_users"),
		SessionStore: auth.NewEntitySessionStore(db, "auth_sessions"),
	})
	authMgr.Use(auth.NewCorePlugin())
	if err := authMgr.Init(fwApp); err != nil {
		log.Fatal(err)
	}
	// A failed form post with no usable Referer lands back on /login
	// with ?error= instead of a raw JSON body.
	auth.SetDefaultLoginErrorPath("/login")
	fwApp.Use(auth.SessionMiddleware(authMgr))

	// Wire the UI layer: site app + screens + host.
	site := app.NewApp("%[2]s").WithTheme(uitheme.Default())
	site.Register("/", &HomeScreen{}, nil)
	site.Register("/login", &LoginScreen{}, nil)
	site.Register("/register", &RegisterScreen{}, nil)

	fwApp.Mount(uihost.New(site))

	// Run migrations
	migrator := migrate.New(db, migrate.WithTableName("_migrations"), migrate.WithDialect(%[6]s))
	entities.RegisterMigrations(migrator)
	// A migration that cannot apply is a failed deploy, not a warning: the
	// server must not report ready over a schema the committed migrations
	// never reached.
	if err := migrator.Up(context.Background()); err != nil {
		log.Fatalf("migrations: %%v", err)
	}

	addr, err := runtimeIsolation.Addr(getEnv("PORT", "localhost:8080"))
	if err != nil {
		log.Fatal(err)
	}
	// Banner fires via OnReady: only after migrations, hooks, and the
	// port bind all succeeded.
	fwApp.OnReady(func(boundAddr string) {
		fmt.Printf("  %%s Server running at http://%%s\n", "✓", boundAddr)
	})
	if err := fwApp.Start(addr); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
`, modulePath, appName, driverImport, sqlDriver, dsnFallback, migrateDialect)
	}
	if err := writeScaffoldFile(filepath.Join(name, "main.go"), []byte(content)); err != nil {
		cli.Fail("Failed to write main.go: %v", err)
		cli.OsExit(1)
	}
}

// envTrackedByGit reports whether dir/.env is in git's index. No git, or
// no repository, means nothing is tracked.
func envTrackedByGit(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".env")); err != nil {
		return false
	}
	cmd := exec.Command("git", "-C", dir, "ls-files", "--error-unmatch", "--", ".env")
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	return cmd.Run() == nil
}

// writeEnvFile writes the scaffolded .env with owner-only permissions
// (fileperm.ReplaceOwnerOnly).
//
// os.WriteFile would truncate a pre-existing 0644 file and write the
// secrets into it while it is still world-readable, then leave it that
// way; the mode is set on a temporary file first, and the rename means a
// crash or a full disk during `init . --force` leaves the old .env, not
// an empty one.
func writeEnvFile(path, content string) error {
	return fileperm.ReplaceOwnerOnly(path, []byte(content))
}

// writeScaffoldFile writes a scaffold file at 0644. `init . --force` and
// the AGENTS.md, CLAUDE.md and .gitignore merges rewrite files a project
// already holds, and a crash mid-write left them empty: an existing file
// is replaced through a temporary file and keeps its mode
// (atomicfile.Rewrite).
func writeScaffoldFile(path string, data []byte) error {
	return atomicfile.Rewrite(path, data, 0o644)
}

// existingScaffoldFiles lists the files init . would replace that the
// current directory already holds. .gitignore is not one: it is merged.
func existingScaffoldFiles(noEntity bool) []string {
	files := []string{"main.go", "screens.go", ".env", settings.FileName, "AGENTS.md", "CLAUDE.md", "DESIGN.md"}
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

// writeGitignore writes the scaffold's ignore rules, or appends the ones
// an existing .gitignore lacks: a repo's own rules stay, and .env is
// ignored either way.
func writeGitignore(path, content string) error {
	existing, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return writeScaffoldFile(path, []byte(content))
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
	return writeScaffoldFile(path, []byte(merged+add.String()))
}

// writeSettingsFile writes gofastr.settings.yml with the isolation
// section a new project starts from.
func writeSettingsFile(name, _ string) {
	// strategy: fields are intentionally omitted; the resolver currently
	// dispatches by driver, not by strategy name. Re-introduce them only
	// when they actually toggle behavior.
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
	if err := fileperm.ReplaceOwnerOnly(filepath.Join(name, settings.FileName), []byte(content)); err != nil {
		cli.Fail("Failed to write %s: %v", settings.FileName, err)
		cli.OsExit(1)
	}
}

// writeScreensGo generates screens.go: the sample HomeScreen as owned package
// main, beside main.go at the module root, with no screens/ subpackage, matching
// the scaffold-and-own flat layout in project-structure.md. The scaffold owns
// no CSS or hand-rolled structural markup.
func writeScreensGo(name, appName string, noEntity bool) {
	// The entity hint is prose for the Section description; the CodeBlock
	// stays commands-only so nothing nonsensical is copy-pasteable.
	var entityHint, actions string
	if !noEntity {
		actions = `
				Actions: ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
					ui.LinkButton(ui.LinkButtonConfig{Label: "Sign in", Href: "/login"}),
					ui.LinkButton(ui.LinkButtonConfig{Label: "Create account", Href: "/register", Variant: ui.ButtonSecondary}),
				),`
		entityHint = " The sample posts entity lives in entities/entities.go and serves JSON at /api/posts once you sign in."
	}
	content := fmt.Sprintf(`package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type HomeScreen struct{}

func (h *HomeScreen) Render() render.HTML {
	return ui.Container(ui.ContainerConfig{Width: ui.ContainerNarrow},
		ui.Stack(ui.StackConfig{Gap: ui.GapXL},
			ui.PageHeader(ui.PageHeaderConfig{
				Eyebrow:  "GoFastr",
				Title:    "%[1]s",
				Subtitle: "Your app is running. Replace this screen with a composition chosen from DESIGN.md.",%[3]s
			}),
			ui.Section(ui.SectionConfig{
				Heading:     "Start with intent",
				Description: "Complete DESIGN.md before selecting components, then use the composition recipes embedded in the GoFastr CLI.%[2]s",
			}, ui.CodeBlock(ui.CodeBlockConfig{
				Code:     "gofastr docs ui-composition-recipes\ngofastr theme init",
				Language: "shell",
			})),
		),
	)
}

// No ScreenTitle here on purpose: the page renderer composes
// "<title>SCREEN — APP NAME</title>", so a home screen titled after the
// app itself ships "myapp — myapp". Untitled, the tab reads exactly the
// app name. Add ScreenTitle (and ScreenDescription) on inner pages,
// where the screen name differs from the site's.
func (h *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }
`, appName, entityHint, actions)
	if err := writeScaffoldFile(filepath.Join(name, "screens.go"), []byte(content)); err != nil {
		cli.Fail("Failed to write screens.go: %v", err)
		cli.OsExit(1)
	}
}

// newJWTSecret mints the scaffold's session-signing key: 32 bytes from
// crypto/rand, hex-encoded, written only to the 0600 gitignored .env.
func newJWTSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		cli.Fail("Failed to generate JWT_SECRET: %v", err)
		cli.OsExit(1)
	}
	return hex.EncodeToString(b)
}

// writeAuthScreensGo generates screens_auth.go: the /login and /register
// screens over battery/auth's form endpoints, composed from ui.AuthCard
// with no CSS of their own.
func writeAuthScreensGo(name string) {
	const content = `package main

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type LoginScreen struct{ component.ContextOnly }

func (s *LoginScreen) ScreenTitle(ctx context.Context) string       { return "Sign in" }
func (s *LoginScreen) ScreenDescription(ctx context.Context) string { return "" }
func (s *LoginScreen) ScreenType() app.ScreenType                  { return app.ScreenPage }

func (s *LoginScreen) RenderCtx(ctx context.Context) render.HTML {
	q := app.QueryFromContext(ctx)
	var notice render.HTML
	if q.Get("registered") == "1" {
		notice = render.Text("Account created. Sign in to continue.")
	}
	// A screen behind auth.SessionPolicy sends the visitor here with
	// ?next=<where they were going>; carry it through the form. The auth
	// battery redirects only to a same-site relative path, whatever the
	// form posts.
	next := q.Get("next")
	if next == "" {
		next = "/"
	}
	return ui.Container(ui.ContainerConfig{Width: ui.ContainerNarrow},
		ui.AuthCard(ui.AuthCardConfig{
			Title:  "Sign in",
			Alert:  authError(ctx),
			Notice: notice,
			Body: ui.Form(ui.FormConfig{Action: "/auth/login", Method: "POST", SubmitLabel: "Sign in", SubmitFullWidth: true},
				html.Input(html.InputConfig{Type: "hidden", Name: "next", Value: next}),
				emailField(),
				passwordField("current-password", 0),
			),
			Footer: ui.Link(ui.LinkConfig{Href: "/register", Text: "Create an account"}),
		}),
	)
}

type RegisterScreen struct{ component.ContextOnly }

func (s *RegisterScreen) ScreenTitle(ctx context.Context) string       { return "Create account" }
func (s *RegisterScreen) ScreenDescription(ctx context.Context) string { return "" }
func (s *RegisterScreen) ScreenType() app.ScreenType                  { return app.ScreenPage }

// Registering does not sign the user in. battery/auth answers a new
// address and an already-registered one with the same redirect, so the
// form cannot be used to learn who has an account; a session cookie set
// only for new accounts would give that away. The user lands on /login
// with a notice and signs in.
func (s *RegisterScreen) RenderCtx(ctx context.Context) render.HTML {
	return ui.Container(ui.ContainerConfig{Width: ui.ContainerNarrow},
		ui.AuthCard(ui.AuthCardConfig{
			Title: "Create your account",
			Alert: authError(ctx),
			Body: ui.Form(ui.FormConfig{Action: "/auth/register", Method: "POST", SubmitLabel: "Create account", SubmitFullWidth: true},
				html.Input(html.InputConfig{Type: "hidden", Name: "next", Value: "/login?registered=1"}),
				emailField(),
				passwordField("new-password", 8),
			),
			Footer: ui.Link(ui.LinkConfig{Href: "/login", Text: "Already have an account? Sign in"}),
		}),
	)
}

func emailField() render.HTML {
	return ui.FormField(ui.FormFieldConfig{Label: "Email", For: "auth-email", Required: true, Input: func(c headless.FieldControl) render.HTML {
		return ui.Control(ui.ControlConfig{Field: c, Type: "email", Name: "email", AutoComplete: "email"})
	}})
}

// minLength mirrors battery/auth's 8-character rule on the register
// form only: a sign-in form leaves the check to the server.
func passwordField(autocomplete string, minLength int) render.HTML {
	return ui.FormField(ui.FormFieldConfig{Label: "Password", For: "auth-password", Required: true, Input: func(c headless.FieldControl) render.HTML {
		return ui.Control(ui.ControlConfig{Field: c, Type: "password", Name: "password", AutoComplete: autocomplete, MinLength: minLength})
	}})
}

// authError maps the ?error= code battery/auth redirects back with to the
// message the card shows. The copy is yours to change.
func authError(ctx context.Context) render.HTML {
	switch app.QueryFromContext(ctx).Get("error") {
	case "":
		return ""
	case "invalid_credentials":
		return render.Text("Invalid email or password.")
	case "credentials_required":
		return render.Text("Enter your email and password.")
	case "weak_password":
		return render.Text("Use a password of at least 8 characters.")
	case "rate_limit":
		return render.Text("Too many attempts. Wait a moment and try again.")
	default:
		return render.Text("Something went wrong. Try again.")
	}
}
`
	if err := writeScaffoldFile(filepath.Join(name, "screens_auth.go"), []byte(content)); err != nil {
		cli.Fail("Failed to write screens_auth.go: %v", err)
		cli.OsExit(1)
	}
}

func writeEntitiesGo(name string) {
	const content = `package entities

import (
	"github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// RegisterAll registers all entity definitions with the app.
func RegisterAll(app *framework.App) {
	app.Entity("posts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true, Max: ptrFloat(200)},
			{Name: "body", Type: schema.Text},
			{Name: "published", Type: schema.Bool},
		},
		// MCP: true registers posts_list/get/create/update/delete as MCP tools
		// at /mcp (see framework.WithMCP in main.go). The tools reuse the CRUD
		// handlers and this same Exposure, so they are gated exactly like the
		// REST routes: an unauthenticated tools/call gets the same 401, never
		// a bypass.
		//
		// Secure by default: CRUD requires a signed-in session, which
		// battery/auth (wired in main.go) issues at /login. Add
		// Public: true for anonymous access (gofastr docs auth).
		Exposure: &entity.ExposureConfig{CRUD: boolPtr(true), MCP: true},
	})
}

// RegisterMigrations registers all entity migrations.
//
// This migration is deliberately redundant with the entity declaration above:
// App.Start auto-migrates from EntityConfig, so adding a field to Fields is
// enough for the column to appear on the next boot; you do NOT have to edit
// this file to change the schema.
//
// It is scaffolded anyway because versioned migrations are what a real
// deployment reviews and rolls back, and auto-migrate only applies additive
// changes. Keep both and let auto-migrate handle development, run
// "gofastr migrate generate" to turn the current declaration into an explicit
// migration before a deploy, or delete this function and pass
// framework.WithoutAutoMigrate() to require explicit migrations for everything.
func RegisterMigrations(m *migrate.Migrator) {
	if err := m.Register(migrate.Migration{
		Version: 1,
		Name:    "create_posts",
		Up: ` + "`" + `CREATE TABLE IF NOT EXISTS posts (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			body TEXT DEFAULT '',
			published BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)` + "`" + `,
		Down: ` + "`" + `DROP TABLE IF EXISTS posts` + "`" + `,
	}); err != nil {
		panic(err)
	}
}

func ptrFloat(f float64) *float64 { return &f }

func boolPtr(b bool) *bool { return &b }
`
	if err := writeScaffoldFile(filepath.Join(name, "entities", "entities.go"), []byte(content)); err != nil {
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

// windowsDeviceName reports whether name is one Windows reserves in every
// directory (con, prn, aux, nul, com0-9, lpt0-9). The validator has
// already limited name to lowercase ASCII.
func windowsDeviceName(name string) bool {
	switch name {
	case "con", "prn", "aux", "nul":
		return true
	}
	return len(name) == 4 && (name[:3] == "com" || name[:3] == "lpt") && name[3] >= '0' && name[3] <= '9'
}

// writeCLAUDEmd writes a thin CLAUDE.md that points Claude Code at the
// richer AGENTS.md and the gofastr-host skill. This is the entry point
// Claude Code reads automatically when opening a project.
func writeCLAUDEmd(dir string) error {
	return writeScaffoldFile(filepath.Join(dir, "CLAUDE.md"), claudeMDContent())
}

// claudeMDContent returns the generated CLAUDE.md bytes for comparison.
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
	return writeScaffoldFile(filepath.Join(dir, "DESIGN.md"), designMDContent())
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

// runReinit refreshes AI onboarding files in an existing project.
// It does NOT touch Go source files, go.mod, or git.
//
// Behavior per file:
//   - .agents/*:   always overwritten (framework-owned)
//   - .claude/skills/gofastr-host/*: always overwritten (framework-owned)
//   - AGENTS.md:  uses sync logic (preserves user content outside markers)
//   - CLAUDE.md:  overwrites if unmodified; prompts if user changed it
//   - DESIGN.md:  created when missing, never overwrites app-owned direction
func runReinit(dir string, force bool) {
	fmt.Printf("\n  %s AI onboarding files in %s\n\n", cli.Bold("Refreshing"), cli.Bold(dir))

	// 1. .agents/ detail files: always overwrite.
	if err := agentscmd.WriteAgentDetailFiles(dir); err != nil {
		cli.Fail("Failed to refresh .agents/ details: %v", err)
		cli.OsExit(1)
	}
	cli.Info("  ✓ .agents/ detail files refreshed")

	// 2. .claude/skills/gofastr-host/: always overwrite.
	if err := agentscmd.WriteHostSkill(dir); err != nil {
		cli.Fail("Failed to refresh gofastr-host skill: %v", err)
		cli.OsExit(1)
	}
	cli.Info("  ✓ .claude/skills/gofastr-host/SKILL.md refreshed")

	// 3. AGENTS.md: sync (preserves user content outside markers).
	//
	// AGENTS.md, CLAUDE.md and DESIGN.md are read and written through an
	// os.Root on dir (agentscmd.ReadProjectFile / WriteProjectFile): a
	// symlink is followed only while it stays in the project, so a cloned
	// repo cannot aim a refresh at a file elsewhere on disk.
	existing, err := agentscmd.ReadProjectFile(dir, "AGENTS.md")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		cli.Fail("Cannot read AGENTS.md: %v", err)
		cli.OsExit(1)
		return
	}
	if err != nil {
		// Doesn't exist yet: write fresh.
		if err := agentscmd.WriteProjectFile(dir, "AGENTS.md", agentscmd.BuildAgentsMD()); err != nil {
			cli.Fail("Failed to write AGENTS.md: %v", err)
			cli.OsExit(1)
		}
		cli.Info("  ✓ AGENTS.md created")
	} else {
		refreshed, changed, err := agentscmd.RefreshAgentsMD(existing)
		switch {
		case err != nil && force:
			// --force overwrites customized onboarding files; an AGENTS.md
			// whose markers are gone is one.
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

	// 4. CLAUDE.md: detect user modifications.
	existingClaude, err := agentscmd.ReadProjectFile(dir, "CLAUDE.md")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		cli.Fail("Cannot read CLAUDE.md: %v", err)
		cli.OsExit(1)
		return
	}
	if err != nil {
		// Doesn't exist yet: write fresh.
		if err := agentscmd.WriteProjectFile(dir, "CLAUDE.md", claudeMDContent()); err != nil {
			cli.Fail("Failed to write CLAUDE.md: %v", err)
			cli.OsExit(1)
		}
		cli.Info("  ✓ CLAUDE.md created")
	} else {
		generated := claudeMDContent()
		// A stock CLAUDE.md from a release that wrote agents/ instead
		// of .agents/ is still unmodified: compare against both.
		legacyGenerated := bytes.ReplaceAll(generated, []byte(agentscmd.AgentsDetailDir+"/"), []byte(agentscmd.LegacyAgentsDetailDir+"/"))
		if bytes.Equal(agentscmd.NormalizeMD(existingClaude), agentscmd.NormalizeMD(generated)) ||
			bytes.Equal(agentscmd.NormalizeMD(existingClaude), agentscmd.NormalizeMD(legacyGenerated)) {
			// Unmodified: safe to overwrite.
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
