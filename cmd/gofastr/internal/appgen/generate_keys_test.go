package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// goStructBody returns the field lines of `type name struct { ... }` in
// src, or "" when src declares no such struct.
func goStructBody(src, name string) string {
	_, rest, ok := strings.Cut(src, "type "+name+" struct {\n")
	if !ok {
		return ""
	}
	body, _, _ := strings.Cut(rest, "\n}")
	return body
}

// A composite key's columns name the record in the path, so no patch
// carries them. The create payload sets them, and a batch update item
// names its record by them, each a plain value.
func TestCompositeKeyWriteShapes(t *testing.T) {
	decl := framework.EntityDeclaration{
		Name:       "enrollments",
		PrimaryKey: []string{"student_id", "course_id"},
		Fields: []framework.FieldDeclaration{
			{Name: "student_id", Type: "string", Required: true},
			{Name: "course_id", Type: "string", Required: true},
			{Name: "grade", Type: "string"},
		},
	}
	client := renderClient([]framework.EntityDeclaration{decl})
	typed := renderEntityWriteShapes(decl)
	for _, c := range []struct{ src, name, want string }{
		{client, "EnrollmentsPatch", "\tGrade *string `json:\"grade,omitempty\"`"},
		{typed, "EnrollmentsPatch", "\tGrade *string `json:\"grade,omitempty\"`"},
		{client, "EnrollmentsInput", "\tStudentId string `json:\"studentId\"`\n\tCourseId string `json:\"courseId\"`\n\tGrade *string `json:\"grade,omitempty\"`"},
		{typed, "EnrollmentsInput", "\tStudentId string `json:\"studentId\"`\n\tCourseId string `json:\"courseId\"`\n\tGrade *string `json:\"grade,omitempty\"`"},
		{client, "EnrollmentsBatchPatch", "\tStudentId string `json:\"studentId\"`\n\tCourseId string `json:\"courseId\"`\n\tGrade *string `json:\"grade,omitempty\"`"},
	} {
		if got := goStructBody(c.src, c.name); got != c.want {
			t.Errorf("%s =\n%s\nwant\n%s", c.name, got, c.want)
		}
	}

	spec := sdkSpec{
		App: "app", SDKVersion: "0.0.0", GofastrVersion: "dev", Module: "local/app-sdk",
		Decls:    []framework.EntityDeclaration{decl},
		Entities: []cliEntity{buildEntityModel(decl, cliVerbs)},
	}
	var dts string
	for _, f := range renderSDKJSFiles(spec) {
		if f.Name == "client.d.ts" {
			dts = f.Content
		}
	}
	for _, want := range []string{
		`export type EnrollmentsPatch = Partial<Omit<EnrollmentsInput, "studentId" | "courseId">>;`,
		`Resource<Enrollments, EnrollmentsInput, EnrollmentsPatch, readonly [studentId: string, courseId: string], { studentId: string; courseId: string }>`,
	} {
		if !strings.Contains(dts, want) {
			t.Errorf("client.d.ts lacks %s", want)
		}
	}
}

// Each key column takes its own Go parameter: one that would shadow an
// imported package, or repeat another column's, moves aside.
func TestKeyParamsDistinct(t *testing.T) {
	for key, want := range map[string]string{
		"framework,slug": "frameworkKey string, slug string",
		"ctx,ctx_key":    "ctxKey string, ctxKeyKey string",
		"a_b,aB":         "aB string, aBKey string",
	} {
		decl := framework.EntityDeclaration{Name: "things", PrimaryKey: strings.Split(key, ",")}
		if got := goKeyParams(decl); got != want {
			t.Errorf("key %s: params %q, want %q", key, got, want)
		}
	}
}
