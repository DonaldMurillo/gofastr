package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

func TestVersionCheckApplies(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		args []string
		want bool
	}{
		{"dev", nil, true},
		{"build", []string{"--pkg", "./cmd/app"}, true},
		{"dev", []string{"--help"}, false},
		{"migrate", []string{"up", "-h"}, false},
		{"dve", nil, false},
		{"init", []string{"app"}, false},
		{"upgrade", nil, false},
		{"version", nil, false},
	} {
		if got := versionCheckApplies(tc.cmd, tc.args); got != tc.want {
			t.Errorf("versionCheckApplies(%q, %q) = %v; want %v", tc.cmd, tc.args, got, tc.want)
		}
	}
}

func TestProjectDirFlag(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		args []string
		want string
	}{
		{"dev", []string{"--dir=../app"}, "../app"},
		{"dev", []string{"--addr", ":9000", "--dir", "/srv/app"}, "/srv/app"},
		{"dev", nil, ""},
		{"build", []string{"--dir=../app"}, ""},
		{"dev", []string{"--dir=./default", "--dir=/srv/app"}, "/srv/app"},
		{"dev", []string{"--dir", "./default", "--dir=/srv/app"}, "/srv/app"},
	} {
		if got := projectDirFlag(tc.cmd, tc.args); got != tc.want {
			t.Errorf("projectDirFlag(%q, %q) = %q; want %q", tc.cmd, tc.args, got, tc.want)
		}
	}
}

// Every command dispatch's switch names is either checked or left out
// on purpose, so a new command cannot slip past the decision. The
// commands are read from main.go's dispatch function itself.
func TestVersionCheckCoversEveryCommand(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	skipped := map[string]bool{"init": true, "version": true, "docs": true, "doc": true, "upgrade": true}
	seen := 0
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "dispatch" {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range cc.List {
				lit, ok := e.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				cmd, _ := strconv.Unquote(lit.Value)
				seen++
				if versionCheckedCommands[cmd] == skipped[cmd] {
					t.Errorf("command %q is both checked and skipped, or neither", cmd)
				}
			}
			return true
		})
	}
	if seen < 20 {
		t.Fatalf("found %d commands in dispatch; the parse missed the switch", seen)
	}
}
