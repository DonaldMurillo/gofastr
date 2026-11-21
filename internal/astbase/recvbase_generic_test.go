package astbase

import "testing"

func TestRecvBaseNameGeneric(t *testing.T) {
	fns := funcsOf(t, `package p
func Free()                 {}
func (r rec) Value()        {}
func (r *rec) Pointer()     {}
func (r rec[P]) GenericV()  {}
func (r *rec[P, Q]) GenPtr(){}
func (r (*rec)) Paren()     {}
func (r **rec) DoublePtr()  {}
`)
	cases := []struct{ name, want string }{
		{"Free", ""},
		{"Value", "rec"},
		{"Pointer", "rec"},
		{"GenericV", "rec"},
		{"GenPtr", "rec"},
		{"Paren", "rec"},
		{"DoublePtr", "rec"},
	}
	byName := map[string]string{}
	for _, fn := range fns {
		byName[fn.Name.Name] = RecvBaseNameGeneric(fn)
	}
	for _, c := range cases {
		if got := byName[c.name]; got != c.want {
			t.Errorf("RecvBaseNameGeneric(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}
