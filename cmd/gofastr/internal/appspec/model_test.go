package appspec

import (
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestSystemColumnsExistOnlyWhenTheEntityHasThem(t *testing.T) {
	off := false
	plain := framework.EntityDeclaration{Name: "notes", Timestamps: &off}
	full := framework.EntityDeclaration{Name: "notes", Scope: &fwentity.ScopeDeclaration{SoftDelete: true, MultiTenant: true}}
	for _, col := range []string{"created_at", "updated_at", "deleted_at", "tenant_id"} {
		if _, ok := appSpecColumn(plain, col); ok {
			t.Errorf("%s exists on an entity without timestamps, soft_delete or multi_tenant", col)
		}
		if found, ok := appSpecColumn(full, col); !ok || found != nil {
			t.Errorf("%s: appSpecColumn = %v, %v; want an existing, unflagged column", col, found, ok)
		}
	}
	if _, ok := appSpecColumn(plain, "id"); !ok {
		t.Error("id must always exist")
	}
	related := framework.EntityDeclaration{Name: "comments", Relations: []framework.Relation{{Name: "post", Type: fwentity.RelManyToOne, Entity: "posts", ForeignKey: []string{"post_id"}}}}
	if found, ok := appSpecColumn(related, "post_id"); !ok || found != nil {
		t.Errorf("a relations-block foreign key column: appSpecColumn = %v, %v; want existing and unflagged", found, ok)
	}
}

func TestBlocksDeepIsDepthFirstDocumentOrder(t *testing.T) {
	blocks := []AppSpecBlock{{Text: "a", Children: []AppSpecBlock{{Text: "b", Children: []AppSpecBlock{{Text: "c"}}}, {Text: "d"}}}, {Text: "e"}}
	var got []string
	for _, b := range AppSpecBlocksDeep(blocks) {
		got = append(got, b.Text)
	}
	if want := []string{"a", "b", "c", "d", "e"}; !slices.Equal(got, want) {
		t.Fatalf("AppSpecBlocksDeep order = %q, want %q", got, want)
	}
}

func TestCreateFormFieldOmission(t *testing.T) {
	for _, tc := range []struct {
		field framework.FieldDeclaration
		want  CreateFormFieldStatus
	}{
		{framework.FieldDeclaration{Name: "title"}, CreateFormFieldDrawn},
		{framework.FieldDeclaration{Name: "secret", Hidden: true}, CreateFormFieldHidden},
		{framework.FieldDeclaration{Name: "total", ReadOnly: true}, CreateFormFieldReadOnly},
		{framework.FieldDeclaration{Name: "code", AutoGenerate: "uuid"}, CreateFormFieldReadOnly},
		{framework.FieldDeclaration{Name: "code", AutoGenerate: "none"}, CreateFormFieldDrawn},
		{framework.FieldDeclaration{Name: "id"}, CreateFormFieldServerFilled},
		{framework.FieldDeclaration{Name: "created_at"}, CreateFormFieldServerFilled},
		{framework.FieldDeclaration{Name: "updated_at"}, CreateFormFieldServerFilled},
		{framework.FieldDeclaration{Name: "deleted_at"}, CreateFormFieldServerFilled},
	} {
		if got := CreateFormFieldOmission(tc.field); got != tc.want {
			t.Errorf("CreateFormFieldOmission(%+v) = %d, want %d", tc.field, got, tc.want)
		}
	}
}

func TestDeclKeyIsID(t *testing.T) {
	for _, tc := range []struct {
		key  []string
		want bool
	}{{nil, true}, {[]string{"id"}, true}, {[]string{"slug"}, false}, {[]string{"id", "slug"}, false}} {
		if got := DeclKeyIsID(framework.EntityDeclaration{PrimaryKey: tc.key}); got != tc.want {
			t.Errorf("DeclKeyIsID(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}
