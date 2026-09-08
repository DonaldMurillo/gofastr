package framework

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Pins: the bytes that were hashed are the bytes that exec — no swap window
// between the SHA-256 verify and the exec. (CONTRACT-QUESTION resolved 2026-09-07:
// fd-exec on platforms that support it; the maintainer-confirmed answer.)
// CONTRACT-QUESTION (resolved): reachability of the swap requires write access to the artifact
// inside the host trust boundary, and TrustedProcessRunner documents "crash isolation
// only" — but prepareChildForSpawn's own doc promises "a swapped artifact never reaches
// exec" (§4.6 lift + #37 trust anchor, design §3 decision B), and this test pins that
// doc. If verify-then-exec is accepted as best-effort (TOCTOU inherent to path-based
// exec), fix the doc to say so and delete this test.
// Property: the bytes that were hashed are the bytes that exec — no swap window
// between the SHA-256 verify and the exec.
// Surfaces: processmodule_runner.go::prepareChildForSpawn :221-228 hashes
// d.ArtifactPath (sha256OfFile vs d.ArtifactSHA256) and :246 builds
// exec.Command(d.ArtifactPath); startPreparedChild :289 then calls cmd.Start() on the
// PATH STRING, not on a handle/fd of the hashed bytes — both TrustedProcessRunner and
// SandboxRunner share this sequence.
// Finding: anything that can rename/rewrite the artifact between prepare and start
// (concurrent deploy racing the spawn, a same-host process, a compromised step in the
// SandboxRunner wrap chain, which is explicitly allowed to mutate prep.Cmd between the
// two halves) gets its bytes exec'd under an already-verified digest. The digest never
// travels with the fd, so the #37 trust anchor does not actually hold end-to-end.
// Fix direction: open the artifact once, hash THE open fd, and exec via that same fd
// (fexecve / /dev/fd/N or an equivalent memoized open file), or re-verify the digest
// inside startPreparedChild after any wrap step and refuse/kill on mismatch.
// Test: prepare against the pristine bytes (verify passes), swap a different program
// over the same path between prepare and start, then start — the swapped program must
// never run (start refuses, or the child is killed before its output lands).
const redSwapMarker = "red-swap-swapped-ran"

// TestRunnerRedSwappedArtifact drives the exact hash→exec sequence with a swap
// injected at the true midpoint (between prepareChildForSpawn and
// startPreparedChild, the same window SandboxRunner's backend.Wrap occupies).
func TestRunnerRedSwappedArtifact(t *testing.T) {
	// Pristine artifact: the test binary copied into a scratch dir, digest
	// pinned — exactly what buildChildArtifact gives the supervisor tests.
	path, sha := buildChildArtifact(t)

	spec := ChildSpec{
		Descriptor: ProcessModuleDescriptor{
			Name:           "swapdemo",
			Version:        "1.0.0",
			ArtifactPath:   path,
			ArtifactSHA256: sha,
			TrustTier:      TrustTrusted,
		},
		InstanceID: "red-swap-instance",
	}

	// Verify-then-exec: prepare must pass against the pristine bytes.
	prep, err := prepareChildForSpawn(spec, DefaultChildEnvAllowlist())
	if err != nil {
		t.Fatalf("setup broken: prepare on pristine artifact: %v", err)
	}

	// Between prepare and start, swap the artifact at the pinned path for a
	// DIFFERENT program that touches a marker file then sleeps (so the test
	// can kill it). Atomic rename over the path — the realistic swap.
	markerFile := filepath.Join(t.TempDir(), redSwapMarker)
	swapped := filepath.Join(t.TempDir(), "red-swapped-child")
	script := "#!/bin/sh\nprintf pwned > " + markerFile + "\nsleep 30\n"
	if err := os.WriteFile(swapped, []byte(script), 0o700); err != nil {
		t.Fatalf("setup broken: write swapped program: %v", err)
	}
	if err := os.Rename(swapped, path); err != nil {
		t.Fatalf("setup broken: swap artifact over pinned path: %v", err)
	}

	// Start. If the swapped bytes exec, the marker file appears.
	rc, startErr := startPreparedChild(context.Background(), prep, nil)
	if startErr == nil && rc != nil {
		t.Cleanup(func() {
			_ = rc.Kill()
			_ = rc.Wait()
		})
	}

	// Bounded watch for the swapped program's side effect. Start refusing is
	// the acceptable secure outcome ONLY if the swapped bytes never ran.
	swappedRan := false
	watchDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(watchDeadline) {
		if _, statErr := os.Stat(markerFile); statErr == nil {
			swappedRan = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if swappedRan {
		t.Errorf("SECURITY: [processmodule-runner-toctou] the artifact was swapped between the SHA-256 "+
			"verify (prepareChildForSpawn) and the exec (startPreparedChild) and the SWAPPED program ran "+
			"(marker %q created, startErr=%v) — the digest pins a path, then exec uses the path, so the "+
			"hashed bytes and the exec'd bytes can diverge and the §4.6/#37 'a swapped artifact never "+
			"reaches exec' promise does not hold", markerFile, startErr)
	}
}
