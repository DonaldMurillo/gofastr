package gates

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/core-ui/check"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/internal/sourcewalk"
)

func lintGate[F any](root string, stdout, stderr io.Writer, label string, audit func(string) ([]F, error), format func([]F) string) bool {
	findings, err := audit(root)
	if err != nil {
		cli.Fprintf(stderr, "%s: %v\n", label, err)
		return false
	}
	if len(findings) == 0 {
		return true
	}
	cli.Fprintf(stdout, "%s", format(findings))
	return false
}

func auditTree(root, suffix string, lint func(string) (*check.Result, error)) ([]check.Violation, error) {
	var all []check.Violation
	err := sourcewalk.Walk(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path != filepath.Clean(root) && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, suffix) || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if contracts.IsGeneratedSource(body) {
			return nil
		}
		res, lintErr := lint(path)
		if lintErr != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "" {
			rel = path
		}
		for _, v := range res.Violations {
			all = append(all, check.Violation{File: rel, Line: v.Line, Message: v.Message})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].File != all[j].File {
			return all[i].File < all[j].File
		}
		return all[i].Line < all[j].Line
	})
	return all, nil
}
