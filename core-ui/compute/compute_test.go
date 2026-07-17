package compute

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestRegisterWorker(t *testing.T) {
	reset()
	js := []byte(`self.onmessage = function () {}`)
	RegisterWorker("sum-worker", js)

	asset, ok := LookupWorker("sum-worker")
	if !ok {
		t.Fatal("LookupWorker miss")
	}
	var got bytes.Buffer
	if _, err := asset.WriteTo(&got); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Equal(got.Bytes(), js) {
		t.Fatalf("content=%q want %q", got.Bytes(), js)
	}
	sum := sha256.Sum256(js)
	if want := hex.EncodeToString(sum[:]); asset.Hash() != want {
		t.Fatalf("Hash=%q want %q", asset.Hash(), want)
	}
}

func TestRegisterWASM(t *testing.T) {
	reset()
	wasm := []byte("\x00asm\x01\x00\x00\x00")
	RegisterWASM("sum", wasm)

	asset, ok := LookupWASM("sum")
	if !ok {
		t.Fatal("LookupWASM miss")
	}
	var got bytes.Buffer
	if _, err := asset.WriteTo(&got); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Equal(got.Bytes(), wasm) {
		t.Fatalf("content=%q want %q", got.Bytes(), wasm)
	}
}

func TestRegistrationCopiesBytes(t *testing.T) {
	reset()
	js := []byte("original")
	RegisterWorker("stable", js)
	asset, _ := LookupWorker("stable")
	wantHash := asset.Hash()
	js[0] = 'X'

	asset, _ = LookupWorker("stable")
	var got bytes.Buffer
	_, _ = asset.WriteTo(&got)
	if got.String() != "original" {
		t.Fatalf("content changed to %q", got.String())
	}
	if asset.Hash() != wantHash {
		t.Fatalf("hash changed to %q", asset.Hash())
	}
}

func TestRegistrationRejectsInvalidNames(t *testing.T) {
	invalid := []string{"", "Upper", "has/slash", "has.dot", "has space", strings.Repeat("a", 65)}
	for _, name := range invalid {
		t.Run(name, func(t *testing.T) {
			reset()
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			RegisterWorker(name, []byte("x"))
		})
	}
}

func TestRegistrationConflictPanics(t *testing.T) {
	reset()
	RegisterWorker("worker", []byte("one"))
	RegisterWorker("worker", []byte("one"))

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	RegisterWorker("worker", []byte("two"))
}

func TestWorkerAndWASMShareName(t *testing.T) {
	reset()
	RegisterWorker("sum", []byte("worker"))
	RegisterWASM("sum", []byte("wasm"))

	manifest := Manifest()
	entry, ok := manifest["sum"]
	if !ok {
		t.Fatal("manifest missing sum")
	}
	if entry.JS == "" || entry.WASM == "" {
		t.Fatalf("manifest entry=%+v", entry)
	}
	if _, ok := LookupWorker("sum"); !ok {
		t.Fatal("worker missing")
	}
	if _, ok := LookupWASM("sum"); !ok {
		t.Fatal("wasm missing")
	}
}
