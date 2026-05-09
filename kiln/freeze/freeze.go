// Package freeze emits canonical source artifacts from a Kiln world so
// the in-memory build-mode app can graduate to a regular GoFastr project.
//
// v1 emits two artifacts:
//
//   <dir>/entities/<name>.json — one file per entity in the JSON shape
//                                framework.LoadEntityDeclaration accepts.
//   <dir>/world.json           — full world snapshot (entities + pages +
//                                hooks + routes + seeds + middleware).
//
// Pages, hooks, routes, and seeds aren't yet emittable as Go source. The
// snapshot lets a Kiln session restart from where it left off; further
// codegen lands in later phases.
package freeze

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofastr/gofastr/kiln/world"
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
	if err := writeEntities(w, dir); err != nil {
		return fmt.Errorf("freeze: entities: %w", err)
	}
	if err := writeWorldSnapshot(w, dir); err != nil {
		return fmt.Errorf("freeze: world snapshot: %w", err)
	}
	return nil
}

func writeEntities(w *world.World, dir string) error {
	if len(w.Entities) == 0 {
		return nil
	}
	entDir := filepath.Join(dir, "entities")
	if err := os.MkdirAll(entDir, 0o755); err != nil {
		return err
	}
	for _, ent := range w.Entities {
		if ent == nil {
			continue
		}
		buf, err := json.MarshalIndent(ent, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal %s: %w", ent.Name, err)
		}
		path := filepath.Join(entDir, ent.Name+".json")
		if err := os.WriteFile(path, append(buf, '\n'), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

func writeWorldSnapshot(w *world.World, dir string) error {
	buf, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "world.json"), append(buf, '\n'), 0o644)
}
