package appspec

import (
	"strings"
	"testing"
)

// An owner-scoped entity keyed on columns that leave the owner out has
// a key shared by every account: a second user creating the same key
// got 409 for a row they cannot see, which also told them the key was
// taken. Refused at validate; a key holding the owner, or a column the
// server generates, keeps each user's rows apart.
func TestOwnerScopedKeyNeedsOwner(t *testing.T) {
	spec := func(scope, key, extra string) string {
		return `
app:
  name: Roster
  module: example.com/roster
entities:
  - name: enrollments
    crud: true
` + scope + key + `    fields:
      - name: student_id
        type: string
        required: true
      - name: course_id
        type: string
        required: true
` + extra
	}
	const owner = "    owner_field: user_id\n"
	const userCol = "      - name: user_id\n        type: string\n"
	const uuidCol = "      - name: ref\n        type: uuid\n        auto_generate: uuid\n        read_only: true\n"
	err := r5YAMLAppSpec(t, spec(owner, "    primary_key: [student_id, course_id]\n", userCol))
	if err == nil {
		t.Fatal("an owner-scoped key without the owner was accepted")
	}
	for _, want := range []string{`"enrollments"`, "user_id", "primary_key"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %s: %v", want, err)
		}
	}
	for name, ok := range map[string]string{
		"key holds owner": spec(owner, "    primary_key: [user_id, student_id, course_id]\n", userCol),
		"generated id":    spec(owner, "", userCol),
		"server-made key": spec(owner, "    primary_key: [ref, course_id]\n", uuidCol),
		"no owner_field":  spec("", "    primary_key: [student_id, course_id]\n", ""),
		"unique index":    spec(owner, "    indices:\n      - name: one_per_user\n        columns: [user_id, student_id, course_id]\n        unique: true\n", ""),
	} {
		if err := r5YAMLAppSpec(t, ok); err != nil {
			t.Errorf("%s: refused: %v", name, err)
		}
	}
}
