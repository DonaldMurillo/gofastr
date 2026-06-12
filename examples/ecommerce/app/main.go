package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/examples/ecommerce/app/blueprint"
	"github.com/DonaldMurillo/gofastr/examples/ecommerce/app/entities"
)

func main() {
	runtimeIsolation, err := isolation.Resolve(".")
	if err != nil {
		log.Fatal(err)
	}
	db, err := openBlueprintDB(runtimeIsolation)
	if err != nil {
		log.Fatal(err)
	}
	if db != nil {
		defer db.Close()
	}

	options := []framework.AppOption{framework.WithConfig(framework.AppConfig{Name: blueprint.BlueprintAppName})}
	if db != nil {
		options = append(options, framework.WithDB(db))
	}
	fwApp := framework.NewApp(options...)
	entities.RegisterAll(fwApp)
	fwApp.Router().Handle("POST", "/mcp", fwApp.MCP)
	site := uiapp.NewApp(blueprint.BlueprintAppName)
	blueprint.RegisterGenerated(fwApp, site, db)
	fwApp.Mount(uihost.New(site))
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

func openBlueprintDB(runtimeIsolation *isolation.Runtime) (*sql.DB, error) {
	driver := getEnv("DB_DRIVER", "sqlite")
	dsn := getEnv("DATABASE_URL", "file:shop.db")
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
		return nil, fmt.Errorf("unsupported blueprint db driver %q", driver)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
