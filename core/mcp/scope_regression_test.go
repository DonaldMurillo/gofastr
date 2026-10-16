package mcp

import (
	"math"
	"testing"
)

func TestListHugePageSizeDoesNotOverflow(t *testing.T) {
	s := &Server{listPageSize: math.MaxInt}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	p, c := pageList(s, "tools/list", []int{1, 2, 3}, 1)
	if len(p) != 2 || c != "" {
		t.Fatalf("page=%v cursor=%q", p, c)
	}
}
