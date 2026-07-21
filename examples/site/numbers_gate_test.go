package main

// Gate for the home page's "Numbers you can check" strip. The measured
// values (runtime size, doc count) are computed from the same embedded
// sources the site serves, so they can't drift — this file sanity-checks
// them and pins the values the page states as constants: 5 MCP tools per
// entity and 0 npm packages in the repo.

import (
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestNumbersStripRendersMeasuredValues(t *testing.T) {
	section := string(numbersSection())

	gz := measuredRuntimeGz()
	if !strings.Contains(section, gz) {
		t.Errorf("strip does not render the measured runtime size %q", gz)
	}
	kb, err := strconv.ParseFloat(strings.TrimSuffix(gz, " KB"), 64)
	if err != nil {
		t.Fatalf("measured runtime size %q is not a KB value: %v", gz, err)
	}
	if kb < 5 || kb > 13 {
		t.Errorf("measured runtime gzip = %.1f KB — outside the plausible 5–13 KB band; the budget test and the strip copy both assume ~12 KB", kb)
	}

	count := embeddedDocCount()
	if !strings.Contains(section, count) {
		t.Errorf("strip does not render the embedded doc count %q", count)
	}
	if n, err := strconv.Atoi(count); err != nil || n < 50 {
		t.Errorf("embedded doc count %q — want a number ≥ 50", count)
	}
}

func TestNumbersStripFiveMCPToolsPerEntity(t *testing.T) {
	ent := entity.Define("widgets", entity.EntityConfig{
		Name: "widgets", Table: "widgets",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	srv := mcp.NewServer()
	ch := crud.NewCrudHandler(ent, nil)
	if err := crud.RegisterEntityMCPTools(srv, ch, router.New()); err != nil {
		t.Fatalf("register entity MCP tools: %v", err)
	}
	if got := len(srv.ListTools()); got != 5 {
		t.Fatalf("MCP tools per entity = %d; the home page claims 5 — update both", got)
	}
}

func TestNumbersStripZeroNpmPackages(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "dist", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "package.json" {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) > 0 {
		t.Fatalf("the home page claims 0 npm packages, but the repo contains: %v", found)
	}
}
