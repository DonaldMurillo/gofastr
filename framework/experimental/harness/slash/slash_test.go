package slash

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseBuiltin(t *testing.T) {
	c, err := Parse("/help")
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsBuiltin || c.Name != "help" || c.Namespace != "" {
		t.Errorf("c = %+v", c)
	}
}

func TestParseNamespaced(t *testing.T) {
	c, err := Parse("/skills:gofastr-ui some arg")
	if err != nil {
		t.Fatal(err)
	}
	if c.IsBuiltin {
		t.Error("IsBuiltin should be false")
	}
	if c.Namespace != "skills" || c.Name != "gofastr-ui" {
		t.Errorf("ns=%q name=%q", c.Namespace, c.Name)
	}
	if !reflect.DeepEqual(c.Args, []string{"some", "arg"}) {
		t.Errorf("args = %v", c.Args)
	}
}

func TestParseQuotedArgs(t *testing.T) {
	c, _ := Parse(`/sessions:branch "session id with spaces" --at 5`)
	want := []string{"session id with spaces", "--at", "5"}
	if !reflect.DeepEqual(c.Args, want) {
		t.Errorf("args = %v, want %v", c.Args, want)
	}
}

func TestParseEscapesInsideQuotes(t *testing.T) {
	c, _ := Parse(`/x:y "say \"hi\""`)
	if len(c.Args) != 1 || c.Args[0] != `say "hi"` {
		t.Errorf("args = %v", c.Args)
	}
}

func TestParseRejectsNonSlash(t *testing.T) {
	if _, err := Parse("help"); !errors.Is(err, ErrNotSlashCommand) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseRejectsEmpty(t *testing.T) {
	if _, err := Parse("/"); err == nil {
		t.Fatal("expected error for bare /")
	}
}

func TestParseRejectsEmptyName(t *testing.T) {
	if _, err := Parse("/skills:"); err == nil {
		t.Fatal("expected error for trailing colon")
	}
}

func TestAllBuiltinsHaveNames(t *testing.T) {
	for _, b := range AllBuiltins() {
		if b.Name == "" {
			t.Error("builtin with empty name")
		}
	}
}
