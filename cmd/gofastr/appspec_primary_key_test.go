package main

import (
	"go/ast"
	"go/parser"
	"reflect"
	"slices"
	"strings"
	"testing"
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
	src, err := renderEntityRegistration(decl)
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
	packed, err := packEntityDeclFromCall(call.(*ast.CallExpr))
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

// A primary_key the runtime would refuse at boot is refused when the spec
// loads, naming the problem.
func TestPrimaryKeyRefusedAtLoad(t *testing.T) {
	for name, tc := range map[string]struct{ key, want string }{
		"empty":      {"[]", "non-empty list"},
		"scalar":     {"student_id", "non-empty list"},
		"undeclared": {"[student_id, term]", `"term" is not a declared field`},
		"repeated":   {"[student_id, student_id]", `"student_id" twice`},
		"unkeyable":  {"[student_id, score]", `"score" has a type`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := loadBPWithEntity(t, `  - name: enrollments
    primary_key: `+tc.key+`
    fields:
      - name: student_id
        type: string
      - name: score
        type: float
`)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("primary_key %s: err = %v; want it to mention %q", tc.key, err, tc.want)
			}
		})
	}
}
