package packcmd

import (
	"go/ast"
	"go/parser"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// A foreign_key list reaches Relation.ForeignKey, the generated
// registration, pack, and the YAML round trip, in declared order.
func TestForeignKeyListReachesRegistration(t *testing.T) {
	bp, err := loadBPWithEntity(t, clitest.OrderLinesYAML)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []string{"order_store", "order_no"}
	decl := bp.Entities[1]
	if rel := decl.Relations[0]; !slices.Equal(rel.ForeignKey, want) {
		t.Fatalf("ForeignKey = %v, want %v", rel.ForeignKey, want)
	}
	src, err := appgen.RenderEntityRegistration(decl)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(src, `ForeignKey: []string{"order_store", "order_no"}`) {
		t.Fatalf("registration lacks the foreign key:\n%s", src)
	}
	call, err := parser.ParseExpr(strings.TrimSpace(src[:strings.Index(src, "\n\t_ = ")]))
	if err != nil {
		t.Fatalf("parse registration: %v\n%s", err, src)
	}
	packed, err := appspec.PackEntityDeclFromCall(call.(*ast.CallExpr))
	if err != nil || len(packed.Relations) != 1 || !slices.Equal(packed.Relations[0].ForeignKey, want) {
		t.Fatalf("packed relations = %+v, %v; want ForeignKey %v", packed.Relations, err, want)
	}

	out, err := encodeAppSpecYAML(bp)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	again, err := decodeAppSpecString(out)
	if err != nil {
		t.Fatalf("re-parse: %v\n%s", err, out)
	}
	if got := again.Entities[1].Relations[0].ForeignKey; !slices.Equal(got, want) {
		t.Errorf("YAML round trip ForeignKey = %v; want %v\n%s", got, want, out)
	}
}
