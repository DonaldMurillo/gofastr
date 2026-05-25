package ids

import "testing"

func TestNewIDsAreValid(t *testing.T) {
	if !ValidSession(NewSessionID()) {
		t.Error("NewSessionID not valid")
	}
	if !ValidLog(NewLogID()) {
		t.Error("NewLogID not valid")
	}
	if !ValidCall(NewCallID()) {
		t.Error("NewCallID not valid")
	}
	if !ValidJTI(NewJTI()) {
		t.Error("NewJTI not valid")
	}
	if !ValidClient(NewClientID()) {
		t.Error("NewClientID not valid")
	}
}

func TestParseRejectsWrongPrefix(t *testing.T) {
	s := string(NewLogID()) // log_…
	if _, err := ParseSession(s); err == nil {
		t.Error("ParseSession accepted log_ prefix")
	}
}

func TestRewriteForBranchDeterministic(t *testing.T) {
	src := string(NewCallID())
	dest := NewLogID()
	a, err := RewriteForBranch(src, dest)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RewriteForBranch(src, dest)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("rewrite not deterministic:\n a = %q\n b = %q", a, b)
	}
	// Same prefix preserved.
	if a[:5] != src[:5] {
		t.Errorf("prefix not preserved: %q -> %q", src, a)
	}
	// Different destination -> different output.
	dest2 := NewLogID()
	c, _ := RewriteForBranch(src, dest2)
	if a == c {
		t.Error("rewrite collision across different destinations")
	}
}
