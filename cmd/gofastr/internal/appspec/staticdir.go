package appspec

import (
	"fmt"
	"path"
	"strings"
)

// generatedSourceDirs are the directories, relative to the app's root,
// that `gofastr generate` writes Go into beside the root package: the
// entity packages, the CLI, the codegen output (gofastr.settings.yml
// writes under gen/), and the owned marketing chrome and docs packages.
var generatedSourceDirs = []string{"cmd", "entities", "gen", "siteheader", "sitefooter", "helpdocs"}

// staticDirProblem says why the generated app must not serve dir as its
// public static mount, or returns "". UIHost serves every file under it
// to anonymous callers, so dir must hold none of the app's own files:
//
//   - the app root or a directory above it serves main.go, go.mod, the
//     built binary and the SQLite database beside them;
//   - a directory the generator writes Go into serves that source;
//   - the directory holding the configured SQLite file serves the live
//     database, every row of it.
//
// An absolute dir other than "/" is the deployer's to place and is left
// alone, as is a relative one outside the app (../shared/static).
func staticDirProblem(dir, dbURL string) string {
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

// sqliteFilePath is the cleaned relative path of the database file a
// SQLite DSN names (file:data/app.db?_pragma=…, data/app.db), or "" for
// a network DSN, an in-memory database, or an absolute path.
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
