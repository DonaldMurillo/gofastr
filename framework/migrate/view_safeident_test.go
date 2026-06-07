package migrate

import (
	"strings"
	"testing"
)

// TestViewRender_RejectsUnsafeName pins I2: a view name that isn't a safe SQL
// identifier panics at render rather than being interpolated verbatim into DDL.
func TestViewRender_RejectsUnsafeName(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("render with unsafe view name should panic")
		}
		if !strings.Contains(toString(r), "valid SQL identifier") {
			t.Errorf("panic = %v, want identifier error", r)
		}
	}()
	v := View{Name: "v; DROP TABLE users;--", Select: "SELECT 1"}
	v.render(DialectSQLite)
}

// TestViewRender_AcceptsSafeName confirms a normal view renders.
func TestViewRender_AcceptsSafeName(t *testing.T) {
	v := View{Name: "active_users", Select: "SELECT id FROM users WHERE active"}
	up, down := v.render(DialectSQLite)
	if !strings.Contains(up, "active_users") || !strings.Contains(down, "active_users") {
		t.Errorf("render up=%q down=%q", up, down)
	}
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return ""
}
