package main

// Gate for the home page's "Numbers you can check" strip. The measured
// values (runtime size, doc count) are computed from the same embedded
// sources the site serves, so they can't drift, this file sanity-checks
// them and pins the values the page states as constants: 5 MCP tools per
// entity and 0 npm packages in the repo.

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"

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
	// A sanity band, not the budget: core-ui/runtime/budget_test.go
	// holds the kernel to its byte line (coreGoalGZ). This only proves
	// the strip measured the kernel and not a whole bundle or nothing.
	// The band was raised 14 → 20 KB on 2026-09-26 (spike/layout-parts,
	// parallel part requests replacing the stream reader): the kernel
	// budget lines moved with measured numbers, and the band follows
	// the measured artifact, not the other way round.
	if kb < 5 || kb > 20 {
		t.Errorf("measured runtime gzip = %.1f KB — outside the plausible 5–20 KB band; the kernel budget lives in core-ui/runtime/budget_test.go", kb)
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

// The site and the size gate must measure the same way, or the page
// publishes a friendlier number than the one CI enforces.
//
// They diverged once: budget_test.go moved to DefaultCompression when the
// gate was corrected to the level browsers actually receive, while this page
// stayed on BestCompression and kept publishing the smaller figure. Comparing
// the rendered string against a fresh DefaultCompression measurement pins the
// two together, so the next level change has to move both.
func TestStripMeasuresAtTheGatesLevel(t *testing.T) {
	src, err := runtime.RuntimeJS()
	if err != nil {
		t.Fatalf("RuntimeJS: %v", err)
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {
		t.Fatalf("gzip writer: %v", err)
	}
	if _, err := zw.Write([]byte(src)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	// Compare BYTES, not the rendered string: 12287 (BestCompression) and
	// 12317 (DefaultCompression) both render as "12.0 KB", so a string
	// comparison here would pass at either level and gate nothing.
	if got := runtimeGzBytes(); got != buf.Len() {
		t.Errorf("the strip measures %d gzip bytes but DefaultCompression is %d — "+
			"the page and core-ui/runtime's budget gate are measuring at different levels",
			got, buf.Len())
	}
}
