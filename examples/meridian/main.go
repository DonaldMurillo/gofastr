package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/admin"
	"github.com/DonaldMurillo/gofastr/battery/auth"
	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/dotenv"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/examples/meridian/entities"
)

func main() {
	// Load .env before anything reads the environment — the DB (and
	// its DATABASE_URL) opens before NewApp's own dotenv auto-load
	// would run. Existing process env always wins over the files.
	_ = dotenv.LoadAndApply(".env.local", ".env")
	runtimeIsolation, err := isolation.Resolve(".")
	if err != nil {
		log.Fatal(err)
	}
	db, err := openDB(runtimeIsolation)
	if err != nil {
		log.Fatal(err)
	}
	if db != nil {
		defer db.Close()
	}

	options := []framework.AppOption{framework.WithConfig(framework.AppConfig{Name: appName, APIPrefix: apiPrefix})}
	if db != nil {
		options = append(options, framework.WithDB(db))
	}
	fwApp := framework.NewApp(options...)
	entities.RegisterAll(fwApp)
	fwApp.WithSeed(func(ctx context.Context) error {
		// Seed owner-scoped rows as the bootstrap admin so the demo data
		// belongs to them; a fresh signup then starts with an empty
		// workspace and adds its own. CreateOne stamps the owner column
		// from the user on the context.
		if u, _, err := auth.NewEntityUserStore(db, "auth_users").FindByEmail(ctx, "admin@meridian.dev"); err == nil && u != nil {
			ctx = handler.SetUser(ctx, u)
		}
		for _, s := range seedData() {
			ch, err := fwApp.CrudHandler(s.Entity)
			if err != nil {
				continue
			}
			if n, err := ch.CountAll(ctx, framework.ListOptions{}); err == nil && n > 0 {
				continue
			}
			for _, row := range s.Rows {
				resolveSeedRefs(ctx, fwApp, row)
				if _, err := ch.CreateOne(ctx, row); err != nil {
					log.Printf("seed %s: skipping row: %v", s.Entity, err)
				}
			}
		}
		return nil
	})
	fwApp.Router().Handle("POST", "/mcp", fwApp.MCP)
	site := uiapp.NewApp(appName)
	RegisterGenerated(fwApp, site, db)
	fwApp.Mount(uihost.New(site, uihost.WithStaticDir("static"), uihost.WithCustomCSS(fontFaceCSS+appBaseCSS()+uihost.ReadCustomCSSFile("static/app.css"))))
	fwApp.RegisterBattery(admin.New(admin.Config{PathPrefix: "/admin", Title: appName, AdminRole: "admin", LoginPath: "/login", DB: db, AuditTable: "audit_log", AllEntities: true, Theme: appTheme(), FontFaceCSS: fontFaceCSS}))
	addr, err := runtimeIsolation.Addr(getEnv("PORT", "localhost:8080"))
	if err != nil {
		log.Fatal(err)
	}
	// Banner fires via OnReady — only after auto-migrate, hooks, and the
	// port bind all succeeded. Printing before Start would announce a
	// server that may never come up.
	fwApp.OnReady(func(boundAddr string) {
		fmt.Printf("Server running at http://%s\n", boundAddr)
	})
	if err := fwApp.Start(addr); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func openDB(runtimeIsolation *isolation.Runtime) (*sql.DB, error) {
	driver := getEnv("DB_DRIVER", "sqlite")
	dsn := getEnv("DATABASE_URL", "file:meridian.db")
	resolvedDriver, resolvedDSN, err := runtimeIsolation.Database(driver, dsn)
	if err != nil {
		return nil, err
	}
	driver, dsn = resolvedDriver, resolvedDSN
	switch driver {
	case "", "none":
		return nil, nil
	case "sqlite", "sqlite3":
		return sql.Open("sqlite3", dsn)
	case "postgres", "postgresql":
		return sql.Open("postgres", dsn)
	default:
		return nil, fmt.Errorf("unsupported db driver %q", driver)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// resolveSeedRefs rewrites "@entity.field=value" reference strings in a
// seed row into the resolved primary-key id of the matching row. This lets
// relational seed data point at rows created earlier in the same pass
// (e.g. a subscription's customer_id: "@customers.email=ada@acme.io").
// Unresolvable references are left as-is so the create fails loudly.
func resolveSeedRefs(ctx context.Context, fwApp *framework.App, row map[string]any) {
	for k, v := range row {
		s, ok := v.(string)
		if !ok || !strings.HasPrefix(s, "@") {
			continue
		}
		rest := s[1:]
		dot := strings.IndexByte(rest, '.')
		eq := strings.IndexByte(rest, '=')
		if dot < 1 || eq <= dot+1 {
			continue
		}
		ent, field, val := rest[:dot], rest[dot+1:eq], rest[eq+1:]
		ch, err := fwApp.CrudHandler(ent)
		if err != nil {
			continue
		}
		rows, err := ch.ListAll(ctx, framework.ListOptions{Filters: []filter.ParsedFilter{{Field: field, Op: filter.OpEq, Value: val}}, Limit: 1})
		if err != nil || len(rows) == 0 {
			continue
		}
		if id, ok := rows[0]["id"]; ok {
			row[k] = id
		}
	}
}
