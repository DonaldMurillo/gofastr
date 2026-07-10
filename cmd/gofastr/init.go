package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runInit(args []string) {
	if len(args) == 0 {
		fail("Project name is required.")
		info("Usage: gofastr init <project-name>")
		info("Example: gofastr init myapp")
		osExit(1)
	}

	name := args[0]

	// Validate project name
	if err := validateProjectName(name); err != nil {
		fail("Invalid project name %q: %v", name, err)
		info("Use lowercase letters, digits, and hyphens. Example: my-blog-app")
		osExit(1)
	}

	// Check if directory already exists (skip for "." which is init-in-place)
	if name != "." {
		if _, err := os.Stat(name); err == nil {
			fail("Directory %q already exists.", name)
			info("Use a different name, or cd into the existing directory and run 'gofastr init .'")
			osExit(1)
		}
	}

	// Check for 'go' in PATH
	goPath, err := exec.LookPath("go")
	if err != nil {
		fail("Go is not installed or not in PATH.")
		info("Install Go from https://go.dev/dl/")
		osExit(1)
	}
	_ = goPath

	modulePath := "local/" + name
	dbDriver := "sqlite"
	dbURL := "file:" + name + ".db"
	noEntity := false
	reinit := false
	force := false

	// Allow overriding via flags
	for i := 1; i < len(args); i++ {
		switch {
		case args[i] == "--reinit":
			reinit = true
		case args[i] == "--force":
			force = true
		case strings.HasPrefix(args[i], "--module="):
			modulePath = strings.TrimPrefix(args[i], "--module=")
			if modulePath == "" {
				fail("--module requires a non-empty value.")
				osExit(1)
			}
		case args[i] == "--no-entity":
			noEntity = true
		case strings.HasPrefix(args[i], "--db="):
			dbDriver = strings.TrimPrefix(args[i], "--db=")
			if dbDriver == "postgres" {
				dbURL = "postgres://user:password@localhost:5432/" + name + "?sslmode=disable"
			}
		}
	}

	// --reinit: refresh AI onboarding files only (no Go code, no git).
	if reinit {
		runReinit(name, force)
		return
	}

	fmt.Printf("\n  Creating %s project %s...\n\n", bold("GoFastr"), bold(name))

	// Create directory structure
	dirs := []string{
		name,
		filepath.Join(name, "screens"),
		filepath.Join(name, "static"),
		filepath.Join(name, "gen"),
	}
	if !noEntity {
		dirs = append(dirs,
			filepath.Join(name, "entities"),
			filepath.Join(name, "migrations"),
		)
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fail("Failed to create directory %q: %v", dir, err)
			osExit(1)
		}
	}

	// Write main.go
	writeMainGo(name, modulePath, noEntity, dbDriver, dbURL)

	// Write screens/home.go
	writeHomeScreen(name, noEntity)

	// Write screens/styles.go — CSS via the framework's StyleSheet builder.
	writeStylesGo(name)

	// Write entities/entities.go (only when --no-entity is not set)
	if !noEntity {
		writeEntitiesGo(name)
	}

	// Write .env
	var envContent string
	if noEntity {
		envContent = `# GoFastr Environment Configuration
PORT=localhost:8080
`
	} else {
		envContent = fmt.Sprintf(`# GoFastr Environment Configuration
DATABASE_URL=%s
PORT=localhost:8080
`, dbURL)
	}
	if err := os.WriteFile(filepath.Join(name, ".env"), []byte(envContent), 0o644); err != nil {
		fail("Failed to write .env: %v", err)
		osExit(1)
	}

	writeIsolationConfig(name, dbDriver)

	// Write .gitignore. gen/ holds generated Go (regenerated, not committed);
	// .gofastr/ holds local runtime state (worktree DB isolation, harness).
	gitignoreContent := `gen/
.gofastr/
*.db
.env
bin/
`
	if err := os.WriteFile(filepath.Join(name, ".gitignore"), []byte(gitignoreContent), 0o644); err != nil {
		fail("Failed to write .gitignore: %v", err)
		osExit(1)
	}

	// Write gen/.gitkeep
	if err := os.WriteFile(filepath.Join(name, "gen", ".gitkeep"), []byte(""), 0o644); err != nil {
		fail("Failed to write gen/.gitkeep: %v", err)
		osExit(1)
	}

	// Write migrations/.gitkeep (only with entities)
	if !noEntity {
		if err := os.WriteFile(filepath.Join(name, "migrations", ".gitkeep"), []byte(""), 0o644); err != nil {
			fail("Failed to write migrations/.gitkeep: %v", err)
			osExit(1)
		}
	}

	// Write static/.gitkeep
	if err := os.WriteFile(filepath.Join(name, "static", ".gitkeep"), []byte(""), 0o644); err != nil {
		fail("Failed to write static/.gitkeep: %v", err)
		osExit(1)
	}

	// Write AGENTS.md (thin TOC) + agents/ detail files so AI agents
	// working on this project find the framework primitives instead
	// of reinventing them. Refresh later with `gofastr agents sync`.
	if err := os.WriteFile(filepath.Join(name, "AGENTS.md"), buildAgentsMD(), 0o644); err != nil {
		fail("Failed to write AGENTS.md: %v", err)
		osExit(1)
	}
	if err := writeAgentDetailFiles(name); err != nil {
		fail("Failed to write agents/ details: %v", err)
		osExit(1)
	}

	// Drop the gofastr-host Claude Code skill so AI agents working on
	// this project auto-load the framework's host-app guidance at task
	// start. Refresh manually with `gofastr agents skill`.
	if err := writeHostSkill(name); err != nil {
		fail("Failed to write gofastr-host skill: %v", err)
		osExit(1)
	}

	// Write CLAUDE.md — thin entry point for Claude Code that points
	// agents at the richer AGENTS.md and the gofastr-host skill.
	if err := writeCLAUDEmd(name); err != nil {
		fail("Failed to write CLAUDE.md: %v", err)
		osExit(1)
	}

	// Run go mod init to generate a proper go.mod
	cmd := exec.Command("go", "mod", "init", modulePath)
	cmd.Dir = name
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		info("Could not run 'go mod init': %v", err)
		info("You may need to run 'go mod init %s' manually.", modulePath)
	}

	// Run git init so the project starts with a clean repo.
	gitCmd := exec.Command("git", "init", name)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	if err := gitCmd.Run(); err != nil {
		info("Could not run 'git init': %v", err)
		info("You may need to run 'git init' manually.")
	}

	success("Created project %s in ./%s/", name, name)
	fmt.Println()
	fmt.Printf("  %s:\n", bold("App files"))
	fmt.Println("    main.go              — Application entry point (UI + CSS)")
	fmt.Println("    screens/home.go      — Sample UI page served at /")
	fmt.Println("    screens/styles.go    — CSS via theme tokens + StyleSheet")
	if !noEntity {
		fmt.Println("    entities/entities.go — Sample entity (posts) served at /posts")
	}
	fmt.Println("    .env                 — Environment configuration")
	fmt.Println("    gofastr.yml          — Project configuration")
	fmt.Println("    .gitignore           — Git ignore rules")
	fmt.Println()
	fmt.Printf("  %s (read by AI coding tools so they reach for framework primitives instead of reinventing):\n", bold("AI-agent onboarding"))
	fmt.Println("    CLAUDE.md            — Claude Code entry point; links to AGENTS.md + skill")
	fmt.Println("    AGENTS.md            — Top-level TOC; refresh with `gofastr agents sync`")
	fmt.Println("    agents/              — Auto-generated per-battery detail files linked from AGENTS.md")
	fmt.Println("    .claude/skills/      — Claude Code skill (safe to delete if you only use Cursor/Aider)")
	fmt.Println()
	fmt.Printf("  %s:\n", bold("Next steps"))
	fmt.Printf("    cd %s\n", name)
	fmt.Println("    go mod tidy          — Resolve dependencies")
	fmt.Println("    gofastr dev          — Start development server with hot-reload")
	fmt.Println()
	info("Also try:")
	info("    gofastr docs          — Browse/search framework docs (embedded in the binary)")
	info("    gofastr theme init    — Scaffold a typed theme.go")
	info("    gofastr build         — Build production binary")
	fmt.Println()
	fmt.Println("  Note: `go mod tidy` resolves gofastr from the Go module proxy;")
	fmt.Println("  pin a tagged release. Only add a `replace` directive pointing at")
	fmt.Println("  a local clone if you are hacking on the framework itself.")
	fmt.Println()
}

// writeMainGo generates the application entry point.
func writeMainGo(name, modulePath string, noEntity bool, dbDriver, dbURL string) {
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
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/framework/uihost"

	"%[1]s/screens"
)

func main() {
	runtimeIsolation, err := isolation.Resolve(".")
	if err != nil {
		log.Fatal(err)
	}

	fwApp := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "%[2]s"}),
	)

	// Wire the UI layer: site app + home screen + host.
	site := app.NewApp("%[2]s")
	site.Register("/", &screens.HomeScreen{}, nil)

	css := screens.CreateStyleSheet()
	fwApp.Mount(uihost.New(site, uihost.WithCustomCSS(css)))

	addr, err := runtimeIsolation.Addr(getEnv("PORT", "localhost:8080"))
	if err != nil {
		log.Fatal(err)
	}
	// Banner fires via OnReady — only after migrations, hooks, and the
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
`, modulePath, name)
	} else {
		driverImport := `_ "github.com/mattn/go-sqlite3"`
		migrateDialect := "migrate.DialectSQLite"
		sqlDriver := "sqlite3"
		if dbDriver == "postgres" {
			driverImport = `_ "github.com/lib/pq"`
			migrateDialect = "migrate.DialectPostgres"
			sqlDriver = "postgres"
		}
		content = fmt.Sprintf(`package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	%[3]s

	"%[1]s/entities"
	"%[1]s/screens"
)

func main() {
	runtimeIsolation, err := isolation.Resolve(".")
	if err != nil {
		log.Fatal(err)
	}
	driver, dsn, err := runtimeIsolation.Database("%[4]s", getEnv("DATABASE_URL", "%[5]s"))
	if err != nil {
		log.Fatal(err)
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	fwApp := framework.NewApp(
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "%[2]s"}),
	)

	// Register entities from the entities package.
	entities.RegisterAll(fwApp)

	// Wire the UI layer: site app + home screen + host.
	site := app.NewApp("%[2]s")
	site.Register("/", &screens.HomeScreen{}, nil)

	css := screens.CreateStyleSheet()
	fwApp.Mount(uihost.New(site, uihost.WithCustomCSS(css)))

	// Run migrations
	migrator := migrate.New(db, migrate.WithTableName("_migrations"), migrate.WithDialect(%[6]s))
	entities.RegisterMigrations(migrator)
	if err := migrator.Up(context.Background()); err != nil {
		log.Printf("Migration warning: %%v", err)
	}

	addr, err := runtimeIsolation.Addr(getEnv("PORT", "localhost:8080"))
	if err != nil {
		log.Fatal(err)
	}
	// Banner fires via OnReady — only after migrations, hooks, and the
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
`, modulePath, name, driverImport, sqlDriver, dbURL, migrateDialect)
	}
	if err := os.WriteFile(filepath.Join(name, "main.go"), []byte(content), 0o644); err != nil {
		fail("Failed to write main.go: %v", err)
		osExit(1)
	}
}

func writeIsolationConfig(name, _ string) {
	// strategy: fields are intentionally omitted — the resolver currently
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
	if err := os.WriteFile(filepath.Join(name, "gofastr.yml"), []byte(content), 0o644); err != nil {
		fail("Failed to write gofastr.yml: %v", err)
		osExit(1)
	}
}

// writeHomeScreen generates screens/home.go using CSS classes instead of
// inline style attributes. The CSS lives in screens/styles.go using the
// framework's StyleSheet builder with theme tokens.
func writeHomeScreen(name string, noEntity bool) {
	var entityHint string
	if !noEntity {
		entityHint = "; entities live in entities/entities.go and serve at /posts"
	}
	content := fmt.Sprintf(`package screens

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type HomeScreen struct{}

func (h *HomeScreen) Render() render.HTML {
	return render.Tag("div", map[string]string{"class": "home-container"},
		render.Tag("h1", nil, render.Text("%[1]s")),
		render.Tag("p", nil, render.Text("Your GoFastr app is running. Edit screens/home.go to replace this page.")),
		render.Tag("p", nil,
			render.Text("Next steps: "),
			render.Tag("code", nil, render.Text("gofastr theme init")),
			render.Text(" scaffolds a typed theme%[2]s.")),
	)
}

func (h *HomeScreen) ScreenTitle() string        { return "%[1]s" }
func (h *HomeScreen) ScreenDescription() string  { return "" }
func (h *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }
`, name, entityHint)
	if err := os.WriteFile(filepath.Join(name, "screens", "home.go"), []byte(content), 0o644); err != nil {
		fail("Failed to write screens/home.go: %v", err)
		osExit(1)
	}
}

// writeStylesGo generates screens/styles.go — the site CSS built with the
// framework's Go-native StyleSheet builder using theme tokens. No raw CSS
// files, no inline style attributes.
func writeStylesGo(name string) {
	const content = `package screens

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// CreateStyleSheet builds the site CSS using the framework's
// theme tokens. Edit this to add your own styles — use
// {colors.primary}, {spacing.md}, etc. to reference tokens.
func CreateStyleSheet() string {
	theme := style.DefaultTheme()
	ss := style.NewStyleSheet(theme)

	ss.Rule("*, *::before, *::after").
		Set("box-sizing", "border-box", "margin", "0", "padding", "0").
		End()

	ss.Rule("body").
		Set("font-family", "{fonts.body}",
			"font-size", "16px",
			"line-height", "1.6",
			"color", "{colors.text}",
			"background", "{colors.background}").
		End()

	ss.Rule(".home-container").
		Set("max-width", "640px",
			"margin", "0 auto",
			"padding", "{spacing.2xl}").
		End()

	ss.Rule(".home-container h1").
		Set("font-size", "1.5rem",
			"font-weight", "700",
			"margin-bottom", "{spacing.md}").
		End()

	ss.Rule(".home-container p").
		Set("margin-bottom", "{spacing.sm}").
		End()

	ss.Rule(".home-container code").
		Set("background", "{colors.surface}",
			"border", "1px solid {colors.border}",
			"padding", "1px 6px",
			"border-radius", "{radii.sm}",
			"font-size", "0.9em",
			"font-family", "{fonts.mono}").
		End()

	return ss.CSS()
}
`
	if err := os.WriteFile(filepath.Join(name, "screens", "styles.go"), []byte(content), 0o644); err != nil {
		fail("Failed to write screens/styles.go: %v", err)
		osExit(1)
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
		CRUD: boolPtr(true),
	})
}

// RegisterMigrations registers all entity migrations.
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
	if err := os.WriteFile(filepath.Join(name, "entities", "entities.go"), []byte(content), 0o644); err != nil {
		fail("Failed to write entities/entities.go: %v", err)
		osExit(1)
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
	return nil
}

// writeCLAUDEmd writes a thin CLAUDE.md that points Claude Code at the
// richer AGENTS.md and the gofastr-host skill. This is the entry point
// Claude Code reads automatically when opening a project.
func writeCLAUDEmd(dir string) error {
	return os.WriteFile(filepath.Join(dir, "CLAUDE.md"), claudeMDContent(), 0o644)
}

// claudeMDContent returns the generated CLAUDE.md bytes for comparison.
func claudeMDContent() []byte {
	const content = `# CLAUDE.md — GoFastr host project

This project uses the [GoFastr](https://github.com/DonaldMurillo/gofastr) framework.

## Start here

- **[AGENTS.md](AGENTS.md)** — TOC of every framework primitive with trigger
  phrases. When your task matches a row, open the linked detail file under
  ` + "`" + `agents/` + "`" + ` for the full shape/import/don't-reinvent breakdown.
- **[.claude/skills/gofastr-host/SKILL.md](.claude/skills/gofastr-host/SKILL.md)**
  — Auto-loaded skill that encodes the "reach for the battery first" rule and
  the import paths you need.

## Framework docs (embedded in the CLI)

The ` + "`" + `gofastr` + "`" + ` binary ships with the full framework reference docs
built in — no internet needed, always matches your installed version.

- ` + "`" + `gofastr docs` + "`" + `                — list every topic
- ` + "`" + `gofastr docs <topic>` + "`" + `        — read a topic's full markdown
- ` + "`" + `gofastr docs --grep <term>` + "`" + `  — search across every topic

## Refreshing after a framework upgrade

` + "`" + `gofastr agents sync` + "`" + ` — refreshes AGENTS.md and agents/ detail files.

## Quick reference

- ` + "`" + `gofastr dev` + "`" + `          — dev server with hot-reload
- ` + "`" + `gofastr build` + "`" + `        — production binary
- ` + "`" + `gofastr docs` + "`" + `          — browse/search framework docs
- ` + "`" + `gofastr agents sync` + "`" + `  — refresh AI-agent onboarding files
- ` + "`" + `gofastr theme init` + "`" + `   — scaffold a typed theme.go
`
	return []byte(content)
}

// runReinit refreshes AI onboarding files in an existing project.
// It does NOT touch Go source files, go.mod, or git.
//
// Behavior per file:
//   - agents/*    — always overwritten (framework-owned)
//   - .claude/skills/gofastr-host/* — always overwritten (framework-owned)
//   - AGENTS.md   — uses sync logic (preserves user content outside markers)
//   - CLAUDE.md   — overwrites if unmodified; prompts if user changed it
func runReinit(dir string, force bool) {
	fmt.Printf("\n  %s AI onboarding files in %s\n\n", bold("Refreshing"), bold(dir))

	// 1. agents/ detail files — always overwrite.
	if err := writeAgentDetailFiles(dir); err != nil {
		fail("Failed to refresh agents/ details: %v", err)
		osExit(1)
	}
	info("  ✓ agents/ detail files refreshed")

	// 2. .claude/skills/gofastr-host/ — always overwrite.
	if err := writeHostSkill(dir); err != nil {
		fail("Failed to refresh gofastr-host skill: %v", err)
		osExit(1)
	}
	info("  ✓ .claude/skills/gofastr-host/SKILL.md refreshed")

	// 3. AGENTS.md — sync (preserves user content outside markers).
	agentsPath := filepath.Join(dir, "AGENTS.md")
	existing, err := os.ReadFile(agentsPath)
	if err != nil {
		// Doesn't exist yet — write fresh.
		if err := os.WriteFile(agentsPath, buildAgentsMD(), 0o644); err != nil {
			fail("Failed to write AGENTS.md: %v", err)
			osExit(1)
		}
		info("  ✓ AGENTS.md created")
	} else {
		refreshed, changed, err := refreshAgentsMD(existing)
		if err != nil {
			fail("AGENTS.md sync failed: %v", err)
			info("  The file may have been edited without preserving the auto-generated markers.")
			info("  Run `gofastr agents init --force` to overwrite, or fix the markers manually.")
			osExit(1)
		}
		if changed {
			if err := os.WriteFile(agentsPath, refreshed, 0o644); err != nil {
				fail("Failed to write AGENTS.md: %v", err)
				osExit(1)
			}
			info("  ✓ AGENTS.md synced (auto section updated, your changes preserved)")
		} else {
			info("  ✓ AGENTS.md already up to date")
		}
	}

	// 4. CLAUDE.md — detect user modifications.
	claudePath := filepath.Join(dir, "CLAUDE.md")
	existingClaude, err := os.ReadFile(claudePath)
	if err != nil {
		// Doesn't exist yet — write fresh.
		if err := os.WriteFile(claudePath, claudeMDContent(), 0o644); err != nil {
			fail("Failed to write CLAUDE.md: %v", err)
			osExit(1)
		}
		info("  ✓ CLAUDE.md created")
	} else {
		generated := claudeMDContent()
		if bytes.Equal(normalizeMD(existingClaude), normalizeMD(generated)) {
			// Unmodified — safe to overwrite.
			if err := os.WriteFile(claudePath, generated, 0o644); err != nil {
				fail("Failed to write CLAUDE.md: %v", err)
				osExit(1)
			}
			info("  ✓ CLAUDE.md refreshed (unchanged from generated)")
		} else if force {
			if err := os.WriteFile(claudePath, generated, 0o644); err != nil {
				fail("Failed to write CLAUDE.md: %v", err)
				osExit(1)
			}
			warn("  ⚠ CLAUDE.md overwritten (--force) — your customizations were replaced")
		} else {
			warn("  ⚠ CLAUDE.md has been modified — not overwriting")
			info("     To overwrite: gofastr init --reinit --force")
			info("     To keep your version: no action needed")
		}
	}

	fmt.Println()
	success("Reinit complete. All AI onboarding files refreshed.")
}
