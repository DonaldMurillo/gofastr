package packcmd

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

const tableRowsAppSpec = `app:
  name: TableRows
  module: example.com/addtest
  db:
    driver: sqlite
    url: file:table-rows.db
entities:
  - name: widgets
    fields:
      - name: title
        type: string
        required: true
screens:
  - name: roster
    route: /roster
    title: Roster
    body:
      - kind: data_table
        props:
          caption: People
          columns: [Name, Role]
          rows:
            - [Ada, Admin]
            - [Grace, "Developer, Ops"]
`

// TestPackTableRowsStayLists runs the public path: generate an app from a
// spec with data-table rows, pack it to a file, then load and regenerate the
// packed file. Each row used to pack as the string "[Ada Admin]".
func TestPackTableRowsStayLists(t *testing.T) {
	dir, _ := generateBaseProject(t, tableRowsAppSpec)
	original, err := appspec.LoadAppSpec(filepath.Join(dir, "gofastr.yml"))
	if err != nil {
		t.Fatal(err)
	}
	packedPath := filepath.Join(t.TempDir(), "packed.yml")
	exitCode := 0
	originalExit := cli.OsExit
	cli.OsExit = func(code int) { exitCode = code }
	clitest.CovT_capStdout(t, func() { RunPack([]string{dir, "-o", packedPath}) })
	cli.OsExit = originalExit
	if exitCode != 0 {
		t.Fatalf("pack exited %d", exitCode)
	}
	packed, err := appspec.LoadAppSpec(packedPath)
	if err != nil {
		t.Fatalf("packed spec does not load: %v\n%s", err, clitest.ReadFile(t, filepath.Dir(packedPath), "packed.yml"))
	}
	if len(packed.Screens) != 1 || len(packed.Screens[0].Body) != 1 {
		t.Fatalf("packed screens = %#v", packed.Screens)
	}
	got := packed.Screens[0].Body[0].Props["rows"]
	want := original.Screens[0].Body[0].Props["rows"]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packed rows = %#v; want %#v", got, want)
	}
	before, err := appgen.RenderAppSpecFiles(original)
	if err != nil {
		t.Fatal(err)
	}
	after, err := appgen.RenderAppSpecFiles(packed)
	if err != nil {
		t.Fatalf("packed spec does not regenerate: %v", err)
	}
	if allScreenContent(after) != allScreenContent(before) {
		t.Fatalf("packed table regenerates differently:\nGOT:\n%s\nWANT:\n%s", allScreenContent(after), allScreenContent(before))
	}
}

// TestPackNestedListsRoundTrip covers the list-in-list shapes the decoder
// keeps: an empty inner list, an inner list of scalars that need quoting,
// and an inner list holding maps, which has no flow spelling.
func TestPackNestedListsRoundTrip(t *testing.T) {
	yml := `app:
  name: Nested
  module: example.com/nested
entities:
  - name: posts
    fields:
      - name: title
        type: string
    properties:
      grid:
        - []
        - [1, "a, b", "x:y", true, 2.5]
        -
          - label: one
          - plain
`
	spec, err := decodeAppSpecString(yml)
	if err != nil {
		t.Fatal(err)
	}
	grid, ok := spec.Entities[0].Properties["grid"].([]any)
	if !ok || len(grid) != 3 {
		t.Fatalf("fixture does not decode to three inner lists: %#v", spec.Entities[0].Properties["grid"])
	}
	out, err := encodeAppSpecYAML(spec)
	if err != nil {
		t.Fatal(err)
	}
	again, err := decodeAppSpecString(out)
	if err != nil {
		t.Fatalf("packed nested lists do not parse: %v\n%s", err, out)
	}
	if !reflect.DeepEqual(spec, again) {
		t.Fatalf("nested list round trip mismatch:\n%s\n--- yaml ---\n%s", clitest.FirstAppSpecDiff(spec, again), out)
	}
	if strings.Contains(out, "\"[") {
		t.Fatalf("an inner list was written as a string:\n%s", out)
	}
}
