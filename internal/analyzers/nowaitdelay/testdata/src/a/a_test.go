package a

import (
	"context"
	"os"
	"testing"
)

func TestBoundLateArgsStartsChild(t *testing.T) {
	if os.Getenv("GOFASTR_BOUND_CHILD") == "1" {
		return
	}
	t.Setenv("GOFASTR_BOUND_CHILD", "1")
	if err := boundLateArgs(context.Background(), os.Args[0]); err != nil {
		t.Fatalf("bounded child did not run: %v", err)
	}
}
