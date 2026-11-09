package crud

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// One test per llm.md gap from #669's parked list: the enum cell split by
// a raw |, sanitizeDefault cutting mid-rune, the tenant note hardcoding
// tenant_id, and per_page/offset missing from the list parameters.

// Enum values joined with "|" split the markdown cell: the pipe is the
// cell delimiter. They are escaped (\|), so one row stays one row.
func TestLLMMD_EnumValuesEscapeCellPipe(t *testing.T) {
	ent := entity.Define("tickets", entity.EntityConfig{
		Name:  "tickets",
		Table: "tickets",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "status", Type: schema.Enum, Values: []string{"open", "in review", "closed"}},
		},
	}.WithTimestamps(false))
	doc := EntityLLMMD(ent)
	line := findLLMMDLine(t, doc, "`status`")
	if !strings.Contains(line, `open\|in review\|closed`) {
		t.Errorf("enum values not escaped in row %q", line)
	}
	// With the escapes removed the row is ONE table row: exactly the six
	// pipes of a five-column row (Field..Notes), none extra.
	if n := strings.Count(strings.ReplaceAll(line, `\|`, ""), "|"); n != 6 {
		t.Errorf("row %q has %d unescaped pipes, want 6 (one per column edge)", line, n)
	}
}

// sanitizeDefault truncates long defaults on a rune boundary: the old
// val[:50] cut mid-character and emitted invalid UTF-8 into the doc.
func TestSanitizeDefaultCutsOnRuneBoundary(t *testing.T) {
	long := strings.Repeat("é", 60) // 120 bytes, 60 runes
	got := sanitizeDefault(long)
	if !utf8.ValidString(got) {
		t.Fatalf("sanitizeDefault emitted invalid UTF-8: %q", got)
	}
	if want := strings.Repeat("é", 50) + "…"; got != want {
		t.Fatalf("sanitizeDefault = %q, want %q", got, want)
	}
	// A multi-byte default short enough is kept whole.
	if got := sanitizeDefault(strings.Repeat("é", 10)); got != strings.Repeat("é", 10) {
		t.Fatalf("short multi-byte default mangled: %q", got)
	}
}

// The multi-tenancy note names the configured tenant column, not the
// hardcoded tenant_id: an entity with Scope.TenantField "org_id" scopes
// by org_id.
func TestLLMMD_TenantNoteUsesConfiguredColumn(t *testing.T) {
	ent := entity.Define("docs", entity.EntityConfig{
		Name:  "docs",
		Table: "docs",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "title", Type: schema.String},
		},
		Scope: &entity.ScopeConfig{MultiTenant: true, TenantField: "org_id"},
	}.WithTimestamps(false))
	doc := EntityLLMMD(ent)
	_, after, ok := strings.Cut(doc, "## Multi-tenancy")
	if !ok {
		t.Fatal("no multi-tenancy section")
	}
	// The doc speaks wire keys everywhere (JSONCase default camel), so
	// org_id appears as orgId — and never as the default tenant_id.
	if !strings.Contains(after, "`orgId`") {
		t.Errorf("tenant note does not name the configured column org_id (wire key orgId):\n%s", after)
	}
	if strings.Contains(after, "`tenant_id`") || strings.Contains(after, "`tenantId`") {
		t.Errorf("tenant note names the default tenant column:\n%s", after)
	}
}

// The list endpoint's parameter table documents ?per_page= and ?offset=,
// the two spellings the route accepts beside page/limit (#669 item 3's
// gap for llm.md).
func TestLLMMD_ListParamsDocumentPerPageOffset(t *testing.T) {
	ent := entity.Define("items", entity.EntityConfig{
		Name:  "items",
		Table: "items",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "title", Type: schema.String},
		},
	}.WithTimestamps(false))
	doc := EntityLLMMD(ent)
	for _, row := range []string{"`per_page`", "`offset`"} {
		if !strings.Contains(doc, "| "+row+" |") {
			t.Errorf("llm.md list parameters omit %s", row)
		}
	}
}

func findLLMMDLine(t *testing.T, doc, needle string) string {
	t.Helper()
	for line := range strings.SplitSeq(doc, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no line containing %q in llm.md:\n%s", needle, doc)
	return ""
}
