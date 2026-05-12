package main

import (
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
		os.Exit(1)
	}

	name := args[0]

	// Validate project name
	if err := validateProjectName(name); err != nil {
		fail("Invalid project name %q: %v", name, err)
		info("Use lowercase letters, digits, and hyphens. Example: my-blog-app")
		os.Exit(1)
	}

	// Check if directory already exists
	if _, err := os.Stat(name); err == nil {
		fail("Directory %q already exists.", name)
		info("Use a different name, or cd into the existing directory and run 'gofastr init .'")
		os.Exit(1)
	}

	// Check for 'go' in PATH
	goPath, err := exec.LookPath("go")
	if err != nil {
		fail("Go is not installed or not in PATH.")
		info("Install Go from https://go.dev/dl/")
		os.Exit(1)
	}
	_ = goPath

	modulePath := "github.com/user/" + name
	dbDriver := "sqlite"
	dbURL := "file:" + name + ".db"

	// Allow overriding via flags
	for i := 1; i < len(args); i++ {
		switch {
		case strings.HasPrefix(args[i], "--module="):
			modulePath = strings.TrimPrefix(args[i], "--module=")
		case strings.HasPrefix(args[i], "--db="):
			dbDriver = strings.TrimPrefix(args[i], "--db=")
			if dbDriver == "postgres" {
				dbURL = "postgres://user:password@localhost:5432/" + name + "?sslmode=disable"
			}
		}
	}

	fmt.Printf("\n  Creating %s project %s...\n\n", bold("GoFastr"), bold(name))

	// Create directory structure
	dirs := []string{
		name,
		filepath.Join(name, "entities"),
		filepath.Join(name, "migrations"),
		filepath.Join(name, "screens"),
		filepath.Join(name, "static"),
		filepath.Join(name, ".gofastr"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fail("Failed to create directory %q: %v", dir, err)
			os.Exit(1)
		}
	}

	// Write main.go — wires CRUD entities AND a minimal UI home page so
	// `/` returns 200 with a friendly placeholder, not 404. New projects
	// can delete the screens import + uihost.Mount(host) calls if they
	// only want a JSON API.
	mainContent := fmt.Sprintf(`package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core/migrate"
	"github.com/gofastr/gofastr/framework"
	"github.com/gofastr/gofastr/framework/uihost"
	_ "github.com/mattn/go-sqlite3"

	"%s/entities"
	"%s/screens"
)

func main() {
	db, err := sql.Open("sqlite3", getEnv("DATABASE_URL", "file:%s.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	fwApp := framework.NewApp(
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "%s"}),
	)

	// Register entities from the entities package.
	entities.RegisterAll(fwApp)

	// Wire the UI layer: site app + home screen + host.
	site := app.NewApp("%s")
	site.Register("/", &screens.HomeScreen{}, nil)
	fwApp.Mount(uihost.New(site))

	// Run migrations
	migrator := migrate.New(db, migrate.WithTableName("_migrations"))
	entities.RegisterMigrations(migrator)
	if err := migrator.Up(context.Background()); err != nil {
		log.Printf("Migration warning: %%v", err)
	}

	addr := getEnv("PORT", "localhost:8080")
	fmt.Printf("  %%s Server starting at http://%%s\n", "✓", addr)
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
`, modulePath, modulePath, name, name, name)

	if err := os.WriteFile(filepath.Join(name, "main.go"), []byte(mainContent), 0o644); err != nil {
		fail("Failed to write main.go: %v", err)
		os.Exit(1)
	}

	// Write screens/home.go — minimal HomeScreen so `/` works on first
	// boot. Implements component.Component + ScreenSpec so app.Register
	// reads its metadata directly.
	homeContent := fmt.Sprintf(`package screens

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core/render"
)

type HomeScreen struct{}

func (h *HomeScreen) Render() render.HTML {
	return render.Tag("div", map[string]string{"style": "padding:2rem;font-family:system-ui,sans-serif;max-width:640px;margin:0 auto"},
		render.Tag("h1", nil, render.Text("%s")),
		render.Tag("p", nil, render.Text("Your GoFastr app is running. Edit screens/home.go to replace this page.")),
		render.Tag("p", nil,
			render.Text("Next steps: "),
			render.Tag("code", nil, render.Text("gofastr theme init")),
			render.Text(" scaffolds a typed theme; entities live in entities/entities.go and serve at /posts.")),
	)
}

func (h *HomeScreen) ScreenTitle() string        { return "%s" }
func (h *HomeScreen) ScreenDescription() string  { return "" }
func (h *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }
`, name, name)
	if err := os.WriteFile(filepath.Join(name, "screens", "home.go"), []byte(homeContent), 0o644); err != nil {
		fail("Failed to write screens/home.go: %v", err)
		os.Exit(1)
	}

	// Write entities/entities.go (registry file)
	entitiesContent := `package entities

import (
	"github.com/gofastr/gofastr/core/migrate"
	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/framework"
)

// RegisterAll registers all entity definitions with the app.
func RegisterAll(app *framework.App) {
	app.Entity("posts", framework.EntityConfig{
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
	m.Register(migrate.Migration{
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
	})
}

func ptrFloat(f float64) *float64 { return &f }

func boolPtr(b bool) *bool { return &b }
`
	if err := os.WriteFile(filepath.Join(name, "entities", "entities.go"), []byte(entitiesContent), 0o644); err != nil {
		fail("Failed to write entities/entities.go: %v", err)
		os.Exit(1)
	}

	// Write .env
	envContent := fmt.Sprintf(`# GoFastr Environment Configuration
DATABASE_URL=%s
PORT=localhost:8080
`, dbURL)
	if err := os.WriteFile(filepath.Join(name, ".env"), []byte(envContent), 0o644); err != nil {
		fail("Failed to write .env: %v", err)
		os.Exit(1)
	}

	// Write .gitignore
	gitignoreContent := `.gofastr/
*.db
.env
bin/
`
	if err := os.WriteFile(filepath.Join(name, ".gitignore"), []byte(gitignoreContent), 0o644); err != nil {
		fail("Failed to write .gitignore: %v", err)
		os.Exit(1)
	}

	// Write .gofastr/.gitkeep
	if err := os.WriteFile(filepath.Join(name, ".gofastr", ".gitkeep"), []byte(""), 0o644); err != nil {
		fail("Failed to write .gofastr/.gitkeep: %v", err)
		os.Exit(1)
	}

	// Write migrations/.gitkeep
	if err := os.WriteFile(filepath.Join(name, "migrations", ".gitkeep"), []byte(""), 0o644); err != nil {
		fail("Failed to write migrations/.gitkeep: %v", err)
		os.Exit(1)
	}

	// Write static/.gitkeep
	if err := os.WriteFile(filepath.Join(name, "static", ".gitkeep"), []byte(""), 0o644); err != nil {
		fail("Failed to write static/.gitkeep: %v", err)
		os.Exit(1)
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

	success("Created project %s in ./%s/", name, name)
	fmt.Println()
	fmt.Printf("  %s:\n", bold("Generated"))
	fmt.Println("    main.go              — Application entry point (UI + entities)")
	fmt.Println("    screens/home.go      — Sample UI page served at /")
	fmt.Println("    entities/entities.go — Sample entity (posts) served at /posts")
	fmt.Println("    .env                 — Environment configuration")
	fmt.Println("    .gitignore           — Git ignore rules")
	fmt.Println()
	fmt.Printf("  %s:\n", bold("Next steps"))
	fmt.Printf("    cd %s\n", name)
	fmt.Println("    go mod tidy          — Resolve dependencies")
	fmt.Println("    gofastr dev          — Start development server with hot-reload")
	fmt.Println()
	fmt.Println("  Also try:")
	fmt.Println("    gofastr theme init   — Scaffold a typed theme.go")
	fmt.Println("    gofastr build        — Build production binary")
	fmt.Println()
	fmt.Println("  Note: gofastr is pre-alpha and unpublished. If `go mod tidy`")
	fmt.Println("  fails with \"Repository not found\", add a `replace` directive")
	fmt.Println("  pointing at your local clone:")
	fmt.Println("    go mod edit -replace github.com/gofastr/gofastr=/path/to/clone")
	fmt.Println()
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
