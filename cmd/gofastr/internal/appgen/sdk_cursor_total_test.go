package appgen

import (
	"strings"
	"testing"
)

// The cursor list path never counts rows, so its body has no total.
// client.d.ts declared total as always present, so TypeScript typed an
// undefined as a number.
func TestSDKCursorTotalIsOptional(t *testing.T) {
	_, block, ok := strings.Cut(dtsRuntimePrelude, "export interface CursorPage<T> {")
	if !ok {
		t.Fatal("client.d.ts declares no CursorPage")
	}
	block, _, _ = strings.Cut(block, "}")
	if !strings.Contains(block, "total?: number;") || strings.Contains(block, "total: number;") {
		t.Errorf("CursorPage.total must be optional:%s", block)
	}
}
