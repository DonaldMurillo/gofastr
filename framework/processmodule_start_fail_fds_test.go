package framework

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

// A spawn that fails after cmd.Start (here, the codec) closes the
// parent-side stdin, stdout and stderr pipes it prepared (2026-11-01
// review): only the child was killed and reaped, and the three pipe ends
// leaked on every such failure.
func TestStartFailClosesParentPipes(t *testing.T) {
	path, sha := buildChildArtifact(t)
	spec := ChildSpec{
		Descriptor: ProcessModuleDescriptor{
			Name: "codecfail", Version: "1.0.0",
			ArtifactPath: path, ArtifactSHA256: sha, TrustTier: TrustTrusted,
		},
		InstanceID: "codec-fail-instance",
	}
	prep, err := prepareChildForSpawn(spec, DefaultChildEnvAllowlist())
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	want := errors.New("codec refused")
	_, err = startPreparedChild(context.Background(), prep, func(io.Reader, io.Writer, int) (*moduleproto.Codec, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("start err = %v, want the codec error", err)
	}
	for name, c := range map[string]io.Closer{"stdin": prep.Stdin, "stdout": prep.Stdout, "stderr": prep.StderrPipe} {
		if err := c.Close(); !errors.Is(err, os.ErrClosed) {
			t.Errorf("%s pipe was still open after the failed start (Close = %v)", name, err)
		}
	}
}
