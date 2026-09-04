// Package freeze emits canonical source artifacts from a Kiln world so
// the in-memory build-mode app can graduate to a regular GoFastr project.
//
// Freeze emits two artifacts:
//
//	<dir>/gofastr.yml: current one-shot blueprint for owned-Go generation.
//	<dir>/world.json: lossless authoring snapshot, including declarative
//	                    actions that graduate as owned-Go handler stubs.
package freeze

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Freeze writes the world's canonical artifacts under dir. Existing
// files are overwritten.
func Freeze(w *world.World, dir string) error {
	if w == nil {
		return errors.New("freeze: nil world")
	}
	if dir == "" {
		return errors.New("freeze: empty dir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("freeze: mkdir: %w", err)
	}
	if err := writeBlueprint(w, dir); err != nil {
		return fmt.Errorf("freeze: blueprint: %w", err)
	}
	if err := writeWorldSnapshot(w, dir); err != nil {
		return fmt.Errorf("freeze: world snapshot: %w", err)
	}
	return nil
}

func writeBlueprint(w *world.World, dir string) error {
	buf, err := BlueprintYAML(w)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "gofastr.yml"), buf, 0o644)
}

func writeWorldSnapshot(w *world.World, dir string) error {
	// Same substitution as gofastr.yml: the snapshot lands in a
	// directory that is about to be committed, so it carries env
	// references rather than the credentials themselves. Copy first,
	// the caller's live world must not be mutated by freezing it.
	snapshot := *w
	snapshot.App.Auth.JWTSecret = envRef(w.App.Auth.JWTSecret, "JWT_SECRET")
	snapshot.App.Admin.SeedPassword = envRef(w.App.Admin.SeedPassword, "ADMIN_SEED_PASSWORD")
	// A credentialed DSN is credential material too (dbURLRef); a plain
	// SQLite path stays verbatim so the snapshot keeps booting.
	snapshot.App.DBURL = dbURLRef(w.App.DBURL)
	w = &snapshot
	buf, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}
	// 0600: world.json is the complete IR, which includes whatever the
	// session configured. gofastr.yml gets env references instead of
	// values, but the snapshot is the raw world, owner-only.
	path := filepath.Join(dir, "world.json")
	if err := os.WriteFile(path, append(buf, '\n'), 0o600); err != nil {
		return err
	}
	return fileperm.Restrict(path, false)
}
