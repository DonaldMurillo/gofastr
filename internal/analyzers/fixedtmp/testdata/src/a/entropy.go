package a

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

func unrelatedEntropyPath() string {
	var nonce [16]byte
	_, _ = rand.Read(nonce[:])
	return filepath.Join(os.TempDir(), "fixed")
}

func unrelatedEntropyLiteral() string {
	_, _ = rand.Read(make([]byte, 16))
	return "/tmp/fixed"
}

func (server *devServer) unrelatedEntropySuffix() string {
	_, _ = rand.Read(make([]byte, 16))
	return "fixed"
}

func unrelatedEntropyCreates(server *devServer) {
	_ = os.MkdirAll(unrelatedEntropyPath(), 0o700)                                       // want `mkdir on a fixed path under the shared temp root`
	_ = os.WriteFile(unrelatedEntropyLiteral(), nil, 0o600)                              // want `file created at a fixed path under the shared temp root`
	_ = os.MkdirAll(filepath.Join(os.TempDir(), server.unrelatedEntropySuffix()), 0o700) // want `mkdir on a fixed path under the shared temp root`
}

func entropyInUnusedClosure() string {
	unused := func() string { return rand.Text() }
	_ = unused
	return filepath.Join(os.TempDir(), "fixed-closure")
}

func createUnusedClosurePath() error {
	return os.MkdirAll(entropyInUnusedClosure(), 0o700) // want `mkdir on a fixed path under the shared temp root`
}

func entropyInNamedReturn() (path string) {
	var nonce [16]byte
	_, _ = rand.Read(nonce[:])
	path = filepath.Join(os.TempDir(), hex.EncodeToString(nonce[:]))
	return
}

func createNamedReturnPath() error {
	return os.MkdirAll(entropyInNamedReturn(), 0o700)
}

func unrelatedEntropyResult() (string, error) {
	_, err := rand.Read(make([]byte, 16))
	return "fixed-result", err
}

func ignoreRandomArgument(value string) string { return "fixed-argument" }

func createIndirectFixedPaths() {
	suffix, _ := unrelatedEntropyResult()
	_ = os.MkdirAll(filepath.Join(os.TempDir(), suffix), 0o700)                            // want `mkdir on a fixed path under the shared temp root`
	_ = os.MkdirAll(filepath.Join(os.TempDir(), ignoreRandomArgument(rand.Text())), 0o700) // want `mkdir on a fixed path under the shared temp root`
}

func unrelatedEntropyNamedReturn() (path string) {
	_, _ = rand.Read(make([]byte, 16))
	path = filepath.Join(os.TempDir(), "fixed-named-result")
	return
}

func createFixedNamedReturn() error {
	return os.MkdirAll(unrelatedEntropyNamedReturn(), 0o700) // want `mkdir on a fixed path under the shared temp root`
}

func fillRandomSuffix(buffer []byte) string {
	_, _ = rand.Read(buffer)
	return hex.EncodeToString(buffer)
}

func createFilledRandomPath() error {
	buffer := make([]byte, 16)
	return os.MkdirAll(filepath.Join(os.TempDir(), fillRandomSuffix(buffer)), 0o700)
}

func fixedBufferSuffix(buffer []byte) string {
	return hex.EncodeToString(buffer)
}

func createFixedBufferPath() error {
	return os.MkdirAll(filepath.Join(os.TempDir(), fixedBufferSuffix(make([]byte, 16))), 0o700) // want `mkdir on a fixed path under the shared temp root`
}

// sprintfFormatRoot: the temp root is the FORMAT literal, so the tail is
// every argument; a minted argument keeps the path quiet. (The Sprintf
// special case must not kick in: argument 1 is not a temp root.)
func sprintfFormatRoot() error {
	return os.MkdirAll(fmt.Sprintf("/tmp/%s-%d", rand.Text(), os.Getpid()), 0o700)
}

// localMintedJoin: the minted segment is a local var DECLARATION inside
// the helper (var suffix = …), whose value the body walker records.
func localMintedJoin() string {
	var suffix = rand.Text()
	return filepath.Join(os.TempDir(), suffix)
}

func createLocalMintedPath() error {
	return os.MkdirAll(localMintedJoin(), 0o700)
}

// localDeclOnly: a type-only local declaration has no value for its
// name; the walker must skip the names-without-values zip.
func localDeclOnly() {
	var scratch []byte
	_ = len(scratch)
}

// bare declaration with no value: the name/value zip guard must not
// index an absent value.
var declaredOnly []byte
