package bom_test

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/core/dotenv"
	"github.com/DonaldMurillo/gofastr/core/markdown"
	"github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/bom"
)

// TestReadersIgnoreBOM feeds each reader of a hand-written text file the
// same input with and without a leading byte order mark and wants the
// same result. A reader added for a new file kind joins this table.
// core/migrate's parseMigration and the upgrade text matcher are
// unexported and carry the same case in their own packages.
func TestReadersIgnoreBOM(t *testing.T) {
	readers := []struct {
		name string
		in   string
		read func(string) (any, error)
	}{
		{"dotenv", "KEY=value\nOTHER=2\n", func(s string) (any, error) {
			return dotenv.Parse(strings.NewReader(s))
		}},
		{"yaml", "name: shop\nport: 8080\n", func(s string) (any, error) {
			n, err := yaml.Parse(s)
			if err != nil {
				return nil, err
			}
			return n.Map["name"].Value, nil
		}},
		{"markdown", "---\ntitle: Hello\n---\n# Body\n", func(s string) (any, error) {
			d := markdown.Render(s)
			return []any{d.Frontmatter, d.HTML}, nil
		}},
		{"routines", "CREATE OR REPLACE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;\n", func(s string) (any, error) {
			fsys := fstest.MapFS{"r/f.pg.sql": {Data: []byte(s)}}
			return migrate.RoutinesFS(fsys, "r")
		}},
	}
	for _, r := range readers {
		t.Run(r.name, func(t *testing.T) {
			plain, err := r.read(r.in)
			if err != nil {
				t.Fatalf("without a BOM: %v", err)
			}
			marked, err := r.read(bom.UTF8 + r.in)
			if err != nil {
				t.Fatalf("with a BOM: %v", err)
			}
			if !reflect.DeepEqual(plain, marked) {
				t.Errorf("a leading BOM changed the result:\nplain  %#v\nmarked %#v", plain, marked)
			}
		})
	}
}
