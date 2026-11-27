package main

import (
	"bufio"
	"bytes"
	"go/build/constraint"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

func lintChromiumShard(root string) ([]finding, error) {
	const ciPath = ".github/workflows/ci.yml"
	body, err := os.ReadFile(filepath.Join(root, ciPath))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	modBody, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	module := moduleLine(modBody)
	if module == "" {
		return nil, nil
	}
	var wf struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct {
					Include []map[string]string `yaml:"include"`
				} `yaml:"matrix"`
			} `yaml:"strategy"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &wf); err != nil {
		return []finding{{File: ciPath, Line: 1, Rule: "chromium-shard", Message: "ci.yml does not parse: " + err.Error()}}, nil
	}
	var shard map[string]string
	for _, job := range wf.Jobs {
		for _, row := range job.Strategy.Matrix.Include {
			if row["suite"] == "chromium-ui" {
				shard = row
			}
		}
	}
	tagged, err := chromiumTestDirs(root)
	if err != nil {
		return nil, err
	}
	if shard == nil {
		if len(tagged) == 0 {
			return nil, nil
		}
		return []finding{{File: ciPath, Line: 1, Rule: "chromium-shard",
			Message: "no chromium-ui matrix row, so no CI job runs the chromium-tagged tests"}}, nil
	}
	run := shard["run"]
	line := lineContaining(string(body), "suite: chromium-ui")
	var out []finding
	if !strings.Contains(run, "scripts/tagged-tests.sh chromium") {
		out = append(out, finding{File: ciPath, Line: line, Rule: "chromium-shard",
			Message: "the chromium-ui run does not take its -run pattern from scripts/tagged-tests.sh, so it re-runs the untagged tests the main job already ran"})
	}
	pkgRE, err := regexp.Compile(shard["pkg"])
	if err != nil {
		return append(out, finding{File: ciPath, Line: line, Rule: "chromium-shard",
			Message: "the chromium-ui pkg is not a regexp: " + err.Error()}), nil
	}
	fields := strings.FieldsFunc(run, func(r rune) bool {
		return unicode.IsSpace(r) || r == '"' || r == '\'' || r == '='
	})
	for _, dir := range tagged {
		if !shardNames(fields, dir) {
			out = append(out, finding{File: ciPath, Line: line, Rule: "chromium-shard",
				Message: dir + " has chromium-tagged tests the chromium-ui run does not name (add ./" + dir + "/)"})
		}
		if !pkgRE.MatchString(module + "/" + dir) {
			out = append(out, finding{File: ciPath, Line: line, Rule: "chromium-shard",
				Message: dir + " has chromium-tagged tests the chromium-ui pkg pattern does not match, so a change to it skips the shard"})
		}
	}
	return out, nil
}

// shardNames reports whether the run names dir as ./dir, ./dir/ or under ./prefix/....
func shardNames(fields []string, dir string) bool {
	for _, f := range fields {
		if !strings.HasPrefix(f, "./") {
			continue
		}
		f = strings.TrimPrefix(f, "./")
		if prefix, ok := strings.CutSuffix(f, "/..."); ok {
			if dir == prefix || strings.HasPrefix(dir, prefix+"/") {
				return true
			}
			continue
		}
		if strings.TrimSuffix(f, "/") == dir {
			return true
		}
	}
	return false
}

// chromiumTestDirs lists the root module's package dirs, relative to root, holding a chromium-tagged _test.go file.
func chromiumTestDirs(root string) ([]string, error) {
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root {
				if shouldSkipDir(d.Name(), false) || d.Name() == "testdata" {
					return fs.SkipDir
				}
				if _, statErr := os.Stat(filepath.Join(p, "go.mod")); statErr == nil {
					return fs.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !namesBuildTag(body, "chromium") {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(p))
		if err != nil {
			return err
		}
		seen[path.Clean(filepath.ToSlash(rel))] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(seen))
	for d := range seen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// namesBuildTag reports whether the file's //go:build header line names tag positively.
func namesBuildTag(body []byte, tag string) bool {
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "package ") {
			return false
		}
		if !constraint.IsGoBuild(line) {
			continue
		}
		expr, err := constraint.Parse(line)
		if err != nil {
			return false
		}
		return exprNames(expr, tag, false)
	}
	return false
}

func exprNames(e constraint.Expr, tag string, negated bool) bool {
	switch e := e.(type) {
	case *constraint.TagExpr:
		return e.Tag == tag && !negated
	case *constraint.NotExpr:
		return exprNames(e.X, tag, !negated)
	case *constraint.AndExpr:
		return exprNames(e.X, tag, negated) || exprNames(e.Y, tag, negated)
	case *constraint.OrExpr:
		return exprNames(e.X, tag, negated) || exprNames(e.Y, tag, negated)
	}
	return false
}
