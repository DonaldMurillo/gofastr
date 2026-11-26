package a

import (
	"context"
	"lib"
	"testing"
)

func TestDirect(t *testing.T) {
	var ti lib.Titler
	if ti != nil {
		_ = ti.Title(context.Background())
	}
}
