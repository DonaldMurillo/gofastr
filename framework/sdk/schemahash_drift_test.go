package sdk

import (
	"fmt"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// zzRound2Registry is a slice-backed entity.Registry that preserves Version,
// mirroring framework.Registry's (name, version) keying.
type zzRound2Registry struct{ ents []*entity.Entity }

func (r *zzRound2Registry) All() map[string]*entity.Entity {
	m := make(map[string]*entity.Entity, len(r.ents))
	for _, e := range r.ents {
		m[e.Config.Name] = e
	}
	return m
}
func (r *zzRound2Registry) AllSorted() []*entity.Entity { return r.ents }
func (r *zzRound2Registry) Get(name string) (*entity.Entity, error) {
	for _, e := range r.ents {
		if e.Config.Name == name {
			return e, nil
		}
	}
	return nil, fmt.Errorf("entity %q not found", name)
}

func zzRound2PostsConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
		},
	}
}

// TestNoDriftForGroupMountedEntity reproduces the drift banner that can
// never be cleared.
//
// Two halves of the same check disagree:
//
//   - GENERATION (cmd/gofastr/generate_sdk.go sdkSchemaHash) builds
//     NamedConfig{Name, Config} from EntityDeclarations and NEVER sets
//     Version, so the manifest hash is the unversioned one.
//   - SERVING (framework/sdkdocs.loadManifest) builds NamedConfigs via
//     RegistryNamedConfigs, which fills Version from the live entity.
//
// App.GroupEntity sets entity.Version to the group prefix (framework/app.go
// :1129), so ANY app that mounts an entity in a route group produces a live
// hash the manifest can never match. sdkdocs then sets s.drift and logs
// "SDK artifacts were generated from an older schema — re-run
// `gofastr generate sdk`" on every boot, immediately after a fresh
// regeneration. The warning is unfixable and trains readers to ignore a real
// drift signal.
//
// Run:
//
//	go test ./framework/sdk/ -run TestNoDriftForGroupMountedEntity -v
func TestNoDriftForGroupMountedEntity(t *testing.T) {
	cfg := zzRound2PostsConfig()

	// Generation half — exactly what cmd/gofastr's sdkSchemaHash emits.
	// entity.Define applies the same defaults EntityDeclaration.Config() does.
	manifestHash := SchemaHash([]NamedConfig{
		{Name: "posts", Config: entity.Define("posts", cfg).Config},
	})

	// Serving half — one entity, mounted via app.GroupEntity(app.Group("/api/v1"), ...).
	v1 := entity.Define("posts", zzRound2PostsConfig())
	v1.Version = "/api/v1"
	reg := &zzRound2Registry{ents: []*entity.Entity{v1}}
	liveHash := SchemaHash(RegistryNamedConfigs(reg, []string{"posts"}))

	if liveHash != manifestHash {
		t.Fatalf("SDK drift reported for an app that never drifted:\n  manifest (generation side) = %s\n  live (serving side)        = %s\n"+
			"App.GroupEntity stamps Version=%q; the generator never sets Version, so the two hashes can never agree.",
			manifestHash, liveHash, v1.Version)
	}
}
