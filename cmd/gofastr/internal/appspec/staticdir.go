package appspec

import (
	"fmt"
	"path"
	"strings"
)

var generatedSourceDirs = []string{"cmd", "entities", "gen", "siteheader", "sitefooter", "docpage", "helpdocs"}

func staticDirProblem(dir, dbURL, outputDir string) string {
	raw := strings.TrimSpace(dir)
	if raw == "" {
		return ""
	}
	p := path.Clean(strings.ReplaceAll(raw, `\`, "/"))
	if p == "/" || p == "." || p == ".." || (strings.HasPrefix(p, "../") && strings.Trim(p, "./") == "") {
		return "is the app's root or a directory above it, so the app would serve its own main.go, go.mod, built binary and database file to anyone; use a directory of its own such as static"
	}
	if path.IsAbs(p) || strings.HasPrefix(p, "../") {
		return ""
	}
	if out := cleanRelDir(outputDir); out != "" && (p == out || strings.HasPrefix(p, out+"/") || strings.HasPrefix(out, p+"/")) {
		return fmt.Sprintf("overlaps app.output_dir %s, where the generator writes the app's Go, so the app would serve its own source; use a directory of its own such as static", out)
	}
	first, _, _ := strings.Cut(p, "/")
	for _, d := range generatedSourceDirs {
		if first == d {
			return fmt.Sprintf("is inside %s/, where the generator writes Go, so the app would serve its own source; use a directory of its own such as static", d)
		}
	}
	if db := sqliteFilePath(dbURL); db != "" && (db == p || strings.HasPrefix(db, p+"/")) {
		return fmt.Sprintf("holds the database file %s (app.db.url), so the app would serve the live database, every row of it, to anyone; move the database or the static files", db)
	}
	return ""
}

func sqliteFilePath(dsn string) string {
	s := strings.TrimSpace(dsn)
	if s == "" || strings.Contains(s, "://") {
		return ""
	}
	s = strings.TrimPrefix(s, "file:")
	s, _, _ = strings.Cut(s, "?")
	if s == "" || strings.HasPrefix(s, ":memory:") {
		return ""
	}
	p := path.Clean(strings.ReplaceAll(s, `\`, "/"))
	if path.IsAbs(p) || p == "." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

func cleanRelDir(dir string) string {
	raw := strings.TrimSpace(dir)
	if raw == "" {
		return ""
	}
	p := path.Clean(strings.ReplaceAll(raw, `\`, "/"))
	if p == "." || path.IsAbs(p) || p == ".." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}
