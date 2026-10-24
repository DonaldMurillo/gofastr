package crud

import (
	"context"
	"net/url"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// An include through a pivot that two other versions own is refused with
// a phrase, not a raw error.
func TestIncludeAmbiguousPivotPhrased(t *testing.T) {
	students := entity.Define("students", entity.EntityConfig{
		Fields:    []schema.Field{{Name: "id", Type: schema.String}},
		Relations: []entity.Relation{entity.ManyToMany("courses", "courses", "enrollments", "student_id", "course_id")},
	}.WithTimestamps(false))
	students.Version = "/v1"
	courses := entity.Define("courses", entity.EntityConfig{
		Fields: []schema.Field{{Name: "id", Type: schema.String}},
	}.WithTimestamps(false))
	enrollments := func(version string) *entity.Entity {
		e := entity.Define("enrollments", entity.EntityConfig{
			Fields: []schema.Field{{Name: "student_id", Type: schema.String}, {Name: "course_id", Type: schema.String}},
		}.WithTimestamps(false))
		e.Version = version
		return e
	}
	reg := versionedReg{m: map[string]*entity.Entity{
		"students|/v1": students, "courses|": courses,
		"enrollments|/v2": enrollments("/v2"), "enrollments|/v3": enrollments("/v3"),
	}}
	_, err := parseIncludeTreeQ(url.Values{"include": {"courses"}}, students, reg)
	if err == nil {
		t.Fatal("include through an ambiguous pivot was accepted")
	}
	if _, code := i18n.ErrorText(context.Background(), err, reasonInvalidQuery); code != reasonIncludePivot.Code() {
		t.Fatalf("code = %q, want %q (%v)", code, reasonIncludePivot.Code(), err)
	}
}
