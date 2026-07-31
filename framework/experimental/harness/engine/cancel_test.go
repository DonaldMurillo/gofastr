package engine

import (
	"context"
	"errors"
	"testing"
)

func TestCancelTreePropagatesToChildren(t *testing.T) {
	root := NewCancelTree(context.Background())
	child := root.Child()
	grand := child.Child()

	want := errors.New("boom")
	root.Cancel(want)

	for _, c := range []*CancelTree{root, child, grand} {
		select {
		case <-c.Context().Done():
		default:
			t.Fatal("child not cancelled")
		}
		if !errors.Is(c.Cause(), want) {
			t.Errorf("cause = %v, want %v", c.Cause(), want)
		}
	}
}

func TestCancelTreeChildDoesNotCancelParent(t *testing.T) {
	root := NewCancelTree(context.Background())
	child := root.Child()
	child.Cancel(errors.New("child only"))

	select {
	case <-root.Context().Done():
		t.Fatal("parent should not be cancelled")
	default:
	}
}
