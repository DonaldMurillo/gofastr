package packcmd

import (
	"go/ast"
	"go/parser"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

const enrollmentsYAML = `  - name: enrollments
    primary_key: [student_id, course_id]
    fields:
      - name: student_id
        type: string
        required: true
      - name: course_id
        type: string
        required: true
      - name: grade
        type: string
`

// A primary_key in the app spec reaches EntityConfig.PrimaryKey, the
// generated registration, and back through pack, in declared order.
func TestPrimaryKeyReachesRegistration(t *testing.T) {
	bp, err := loadBPWithEntity(t, enrollmentsYAML)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []string{"student_id", "course_id"}
	decl := bp.Entities[0]
	cfg, err := decl.Config()
	if err != nil || !slices.Equal(cfg.PrimaryKey, want) {
		t.Fatalf("Config().PrimaryKey = %v, %v; want %v", cfg.PrimaryKey, err, want)
	}
	src, err := appgen.RenderEntityRegistration(decl)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(src, `PrimaryKey: []string{"student_id", "course_id"},`) {
		t.Fatalf("registration lacks the key:\n%s", src)
	}
	call, err := parser.ParseExpr(strings.TrimSpace(src[:strings.Index(src, "\n\t_ = ")]))
	if err != nil {
		t.Fatalf("parse registration: %v\n%s", err, src)
	}
	packed, err := appspec.PackEntityDeclFromCall(call.(*ast.CallExpr))
	if err != nil || !slices.Equal(packed.PrimaryKey, want) {
		t.Errorf("packed PrimaryKey = %v, %v; want %v", packed.PrimaryKey, err, want)
	}

	out, err := encodeAppSpecYAML(bp)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	again, err := decodeAppSpecString(out)
	if err != nil {
		t.Fatalf("re-parse: %v\n%s", err, out)
	}
	if !reflect.DeepEqual(again.Entities[0].PrimaryKey, want) {
		t.Errorf("YAML round trip PrimaryKey = %v; want %v\n%s", again.Entities[0].PrimaryKey, want, out)
	}
}
