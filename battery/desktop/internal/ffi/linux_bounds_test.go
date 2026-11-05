//gofastr:test-reads /battery/desktop/internal/ffi/call_linux_*.go

package ffi

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxCallBoundsBeforeFFI(t *testing.T) {
	for _, architecture := range []string{"amd64", "arm64"} {
		t.Run(architecture, func(t *testing.T) {
			source, err := os.ReadFile("call_linux_" + architecture + ".go")
			if err != nil {
				t.Fatal(err)
			}
			_, sourceText, _ := strings.Cut(string(source), "\n")
			sourceText = strings.Replace(sourceText, "func callTrampoline()", "func callTrampoline() {}", 1)
			directory := t.TempDir()
			for name, content := range map[string]string{
				"go.mod":         "module ffibounds\n\ngo 1.22\n",
				"call.go":        sourceText,
				"boundary.go":    "package ffi\nimport \"unsafe\"\nfunc runtime_cgocall(unsafe.Pointer, unsafe.Pointer) { panic(\"native boundary\") }\n",
				"bounds_test.go": linuxBoundsFixture,
			} {
				if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.Command("go", "test", "-count=1", ".")
			command.Dir = directory
			command.Env = append(os.Environ(), "GOWORK=off")
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("Linux argument packing: %v\n%s", err, output)
			}
		})
	}
}

const linuxBoundsFixture = `package ffi
import (
    "testing"
    "unsafe"
)
func TestBounds(t *testing.T) {
    maximum := intRegs + len(callArgs{}.stks)
    for _, hidden := range []bool{false, true} {
        limit := maximum
        var result unsafe.Pointer
        if hidden {
            result = unsafe.Pointer(new(byte))
            if intRegs == 6 { limit-- }
        }
        for _, overflow := range []int{0, 1, 5} {
            func() {
                defer func() {
                    want := "native boundary"
                    if overflow > 0 { want = "ffi: too many integer arguments for one Call" }
                    if got := recover(); got != want { t.Errorf("hidden=%v ints=%d: panic=%v, want %s", hidden, limit+overflow, got, want) }
                }()
                call(0, make([]uintptr, limit+overflow), nil, result)
            }()
        }
    }
    for _, count := range []int{8, 9, 13} {
        func() {
            defer func() {
                want := "native boundary"
                if count > 8 { want = "ffi: too many float arguments for one Call" }
                if got := recover(); got != want { t.Errorf("floats=%d: panic=%v, want %s", count, got, want) }
            }()
            Call(0, nil, make([]float64, count))
        }()
    }
}
`
