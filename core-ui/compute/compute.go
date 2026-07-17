// Package compute registers content-addressed Web Worker and WebAssembly
// assets for GoFastr applications.
package compute

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
)

// Asset is an immutable registered compute asset.
type Asset struct {
	content []byte
	hash    string
}

// Hash returns the asset's SHA-256 content hash as lowercase hexadecimal.
func (a *Asset) Hash() string { return a.hash }

// WriteTo writes the registered bytes without copying them.
func (a *Asset) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(a.content)
	return int64(n), err
}

// Versions contains the registered hashes for one compute asset name.
type Versions struct {
	JS   string `json:"js,omitempty"`
	WASM string `json:"wasm,omitempty"`
}

type entry struct {
	worker *Asset
	wasm   *Asset
}

var (
	mu      sync.RWMutex
	entries = map[string]*entry{}
)

// RegisterWorker registers a self-contained Web Worker script under name.
// Re-registering identical bytes is a no-op; conflicting bytes panic.
func RegisterWorker(name string, js []byte) {
	register(name, js, true)
}

// RegisterWASM registers a WebAssembly module under name. Re-registering
// identical bytes is a no-op; conflicting bytes panic.
func RegisterWASM(name string, wasm []byte) {
	register(name, wasm, false)
}

func register(name string, content []byte, worker bool) {
	if !validName(name) {
		panic(fmt.Sprintf("compute: invalid asset name %q (use 1-64 lowercase letters, digits, '-' or '_')", name))
	}
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])

	mu.Lock()
	defer mu.Unlock()
	e := entries[name]
	if e == nil {
		e = &entry{}
		entries[name] = e
	}
	current := e.wasm
	kind := "WASM"
	if worker {
		current = e.worker
		kind = "worker"
	}
	if current != nil {
		if current.hash == hash && bytes.Equal(current.content, content) {
			return
		}
		panic(fmt.Sprintf("compute.Register%s: duplicate name %q with different content", kind, name))
	}
	asset := &Asset{content: bytes.Clone(content), hash: hash}
	if worker {
		e.worker = asset
	} else {
		e.wasm = asset
	}
}

// LookupWorker returns the registered worker asset for name.
func LookupWorker(name string) (*Asset, bool) {
	mu.RLock()
	defer mu.RUnlock()
	e := entries[name]
	if e == nil || e.worker == nil {
		return nil, false
	}
	return e.worker, true
}

// LookupWASM returns the registered WebAssembly asset for name.
func LookupWASM(name string) (*Asset, bool) {
	mu.RLock()
	defer mu.RUnlock()
	e := entries[name]
	if e == nil || e.wasm == nil {
		return nil, false
	}
	return e.wasm, true
}

// Manifest returns a snapshot of registered names and content hashes.
func Manifest() map[string]Versions {
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[string]Versions, len(entries))
	for name, e := range entries {
		var versions Versions
		if e.worker != nil {
			versions.JS = e.worker.hash
		}
		if e.wasm != nil {
			versions.WASM = e.wasm.hash
		}
		out[name] = versions
	}
	return out
}

func validName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}

func reset() {
	mu.Lock()
	defer mu.Unlock()
	entries = map[string]*entry{}
}
