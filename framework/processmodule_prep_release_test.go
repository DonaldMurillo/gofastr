package framework

import (
	"os"
	"path/filepath"
	"testing"
)

// A failed sandbox Wrap releases the prepared spawn through
// releasePrepFDs. On Linux (exec-by-fd) the prep also holds the verified
// artifact fd; before the fix only the three stdio pipes were closed and
// every failed wrap leaked that fd until GC.
func TestReleasePrepFDsClosesArtifact(t *testing.T) {
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = inR.Close(); _ = outW.Close(); _ = errW.Close() })
	path := filepath.Join(t.TempDir(), "artifact")
	if err := os.WriteFile(path, []byte("bin"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	prep := &childPrep{Stdin: inW, Stdout: outR, StderrPipe: errR, Artifact: artifact}
	releasePrepFDs(prep)
	if _, err := artifact.Stat(); err == nil {
		t.Fatal("artifact fd still open after releasePrepFDs")
	}
	if _, err := inW.Write([]byte("x")); err == nil {
		t.Fatal("stdin pipe still open after releasePrepFDs")
	}
}

// The child-side pipe ends live on prep.Cmd until cmd.Start hands them
// to the child. A spawn abandoned before Start (a Wrap error, a re-hash
// mismatch) must close those too, not only the parent ends.
func TestReleasePrepFDsClosesChildEnds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact")
	body := []byte("#!/bin/sh\n")
	if err := os.WriteFile(path, body, 0o700); err != nil {
		t.Fatal(err)
	}
	sum, err := sha256OfFile(path)
	if err != nil {
		t.Fatal(err)
	}
	spec := ChildSpec{
		Descriptor: ProcessModuleDescriptor{Name: "rel", ArtifactPath: path, ArtifactSHA256: sum},
		InstanceID: "rel-1",
		ScratchDir: t.TempDir(),
	}
	prep, err := prepareChildForSpawn(spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	var ends []*os.File
	for _, s := range []any{prep.Cmd.Stdin, prep.Cmd.Stdout, prep.Cmd.Stderr} {
		f, ok := s.(*os.File)
		if !ok {
			t.Fatalf("child stdio end is %T, want *os.File", s)
		}
		ends = append(ends, f)
	}
	releasePrepFDs(prep)
	for i, f := range ends {
		if _, err := f.Stat(); err == nil {
			t.Errorf("child stdio end %d still open after releasePrepFDs", i)
		}
	}
}
