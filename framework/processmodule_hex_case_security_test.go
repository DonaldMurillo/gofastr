package framework

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/access"
)

func TestRunnerAcceptsHexDigestCase(t *testing.T) {
	descriptor := descriptorForChild(t, childModeEcho)
	descriptor.ArtifactSHA256 = strings.ToUpper(descriptor.ArtifactSHA256)
	child, err := (&TrustedProcessRunner{}).Start(context.Background(), ChildSpec{Descriptor: descriptor, ScratchDir: t.TempDir()})
	if err != nil {
		t.Fatalf("uppercase artifact pin refused: %v", err)
	}
	_ = child.Kill()
	_ = child.Wait()
}

func TestDigestCaseKeepsMigrationStamp(t *testing.T) {
	for _, grants := range [][]access.Permission{{"articles:read"}, {"articles:write"}} {
		store := newTestStore(t)
		stamp := time.UnixMilli(1700000000000)
		if err := store.Install(context.Background(), DesiredState{Module: "demo", ArtifactSHA256: hex256("aa"), EffectiveGrants: []access.Permission{"articles:read"}, MigrationsAppliedAt: &stamp}); err != nil {
			t.Fatal(err)
		}
		result, err := store.InstallOrUpdate(context.Background(), DesiredState{Module: "demo", ArtifactSHA256: strings.ToUpper(hex256("aa")), EffectiveGrants: grants})
		if err != nil || result.Row.MigrationsAppliedAt == nil {
			t.Fatalf("case-only digest change lost approval: result=%+v, error=%v", result, err)
		}
		if grants[0] == "articles:read" && result.Outcome != InstallUnchanged {
			t.Fatalf("case-only digest change upgraded artifact: %v", result.Outcome)
		}
		if stamped, err := store.setMigrationsAppliedFor(context.Background(), "demo", strings.ToUpper(hex256("aa")), stamp); err != nil || !stamped {
			t.Fatalf("case-only stamp pin refused: stamped=%v, error=%v", stamped, err)
		}
	}
}
