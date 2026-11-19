package query

import (
	"strings"
	"testing"
)

func TestSweepQueryBuildArgsOutliveLaterWhere(t *testing.T) {
	qb := Select("id").From("t").Where("a = $1", 1).Limit(5)
	_, args := qb.Build()
	qb.Where("b = $1", 2)
	if len(args) != 2 || args[1] != 5 {
		t.Fatalf("a later Where changed the args Build returned: %v", args)
	}
}

func TestSweepQueryObjectNameTrimsLongerColumn(t *testing.T) {
	col := "c" + strings.Repeat("y", 59)
	if got, want := ObjectName("t", col, "key"), "t_"+col[:57]+"_key"; got != want {
		t.Fatalf("ObjectName(t, long column) = %q, want %q", got, want)
	}
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	if got, want := ObjectName(a, b, "key"), a[:29]+"_"+b[:29]+"_key"; got != want {
		t.Fatalf("ObjectName(equal parts) = %q, want %q", got, want)
	}
	if got, want := ObjectName(a, b, "pkey"), a[:29]+"_"+b[:28]+"_pkey"; got != want {
		t.Fatalf("ObjectName(tied parts) = %q, want %q", got, want)
	}
}
