package sdk

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// NamedConfig pairs an entity name with its config — the generation-side
// input to SchemaHash (declarations converted via EntityDeclaration.Config()
// carry no Name of their own).
type NamedConfig struct {
	Name string
	// Version is the route-group prefix the entity is mounted at (e.g.
	// "/api/v1"), or "" for the unversioned App.Entity path. The serving
	// side fills it from the entity; the generation side leaves it "",
	// because a declaration does not know where the app will mount it.
	//
	// It is NOT part of the hash. It used to be, and that made the check
	// unusable for the apps it mattered most to: App.GroupEntity stamps
	// the group prefix as Version, so any app using a route group reported
	// drift that regenerating could never clear, and a permanently-on
	// drift signal is one nobody reads. The hash answers "does the
	// generated SDK reflect the live schema?" — where a thing is mounted
	// is routing, not schema. Two versions whose schemas genuinely differ
	// still hash differently, because both projections are included.
	Version string
	Config  entity.EntityConfig
}

// SchemaHash returns a deterministic "sha256:<hex>" digest of the
// API-visible schema of the given entities. Both halves of the SDK feature
// compute it: `gofastr generate sdk` records it in the manifest at
// generation time; sdkdocs.Mount recomputes it from the live registry
// (restricted to Manifest.Entities) to warn when the downloadable SDKs no
// longer match the running API.
//
// Every config is normalized through entity.Define before projection, so a
// raw declaration config (no injected id/timestamp columns, empty Table)
// and an already-registered entity hash identically. Define is idempotent
// for this purpose.
//
// The projection covers exactly what changes a generated client's surface:
// name, table (route path), Public, SoftDelete (trashed listing), sorted
// SearchFields (?q=), relations (?include=), and every non-Hidden field's
// name, type, required/unique/readonly/auto flags, enum values, and
// default. Hidden fields never appear on the wire, so flipping one is
// invisible to SDKs and deliberately does not change the hash. Field order
// is sorted away for the same reason.
func SchemaHash(named []NamedConfig) string {
	type hashField struct {
		Name     string   `json:"name"`
		Type     int      `json:"type"`
		Required bool     `json:"required"`
		Unique   bool     `json:"unique"`
		ReadOnly bool     `json:"readOnly"`
		Auto     bool     `json:"auto"`
		NoQuery  bool     `json:"noQuery,omitempty"`
		Values   []string `json:"values,omitempty"`
		Default  any      `json:"default,omitempty"`
		To       string   `json:"to,omitempty"`
		Many     bool     `json:"many,omitempty"`
		// WireName is part of the hash because it changes the client-facing
		// JSON key — a version that renames a field produces a different
		// SDK surface and must signal drift.
		WireName string `json:"wireName,omitempty"`
	}
	type hashRelation struct {
		Type       int    `json:"type"`
		Name       string `json:"name"`
		Entity     string `json:"entity"`
		ForeignKey string `json:"foreignKey,omitempty"`
		Through    string `json:"through,omitempty"`
	}
	type hashEntity struct {
		Name         string         `json:"name"`
		Table        string         `json:"table"`
		Public       bool           `json:"public"`
		SoftDelete   bool           `json:"softDelete"`
		SearchFields []string       `json:"searchFields,omitempty"`
		Fields       []hashField    `json:"fields"`
		Relations    []hashRelation `json:"relations,omitempty"`
	}

	entities := make([]hashEntity, 0, len(named))
	for _, n := range named {
		cfg := entity.Define(n.Name, n.Config).Config

		he := hashEntity{
			Name:       cfg.Name,
			Table:      cfg.Table,
			Public:     cfg.Exposure.Public,
			SoftDelete: cfg.Scope.SoftDelete,
		}
		he.SearchFields = append(he.SearchFields, cfg.SearchFields...)
		sort.Strings(he.SearchFields)

		for _, f := range cfg.Fields {
			if f.Hidden {
				continue
			}
			hf := hashField{
				Name:     f.Name,
				Type:     int(f.Type),
				Required: f.Required,
				Unique:   f.Unique,
				ReadOnly: f.ReadOnly,
				Auto:     f.AutoGenerate != schema.AutoNone,
				// NoQuery changes the generated client's filter surface
				// (CLI flags, the <entity>Fields constant, OpenAPI query
				// params), so flipping it has to move the hash or the
				// drift banner reports "in sync" for a stale SDK.
				NoQuery:  f.NoQuery,
				To:       f.To,
				Many:     f.Many,
				WireName: f.WireName,
			}
			hf.Values = append(hf.Values, f.Values...)
			if f.Default != nil {
				// Round-trip through JSON so equivalent defaults (int 5
				// vs float64 5 from a decoded declaration) hash the same.
				if raw, err := json.Marshal(f.Default); err == nil {
					var v any
					_ = json.Unmarshal(raw, &v)
					hf.Default = v
				}
			}
			he.Fields = append(he.Fields, hf)
		}
		sort.Slice(he.Fields, func(i, j int) bool { return he.Fields[i].Name < he.Fields[j].Name })

		for _, r := range cfg.Relations {
			he.Relations = append(he.Relations, hashRelation{
				Type:       int(r.Type),
				Name:       r.Name,
				Entity:     r.Entity,
				ForeignKey: r.ForeignKey,
				Through:    r.Through,
			})
		}
		sort.Slice(he.Relations, func(i, j int) bool { return he.Relations[i].Name < he.Relations[j].Name })

		entities = append(entities, he)
	}
	// Sort and collapse on the entity's CANONICAL JSON — the same encoding the
	// digest is taken over.
	//
	// This used to use fmt.Sprint, which is not an injective encoding: %v
	// renders []string{"draft ready"} and []string{"draft","ready"} both as
	// [draft ready]. Enum Values are in the projection (they change the
	// generated client's constants) and registry.columnSchemaEqual does not
	// compare them, so two versions may legally declare different enum sets —
	// and a real divergence collapsed silently. It also made the digest
	// depend on input order, since the secondary sort key could not separate
	// the entries it could not tell apart.
	encoded := make([][]byte, len(entities))
	for i, he := range entities {
		b, err := json.Marshal(he)
		if err != nil {
			// Same fallback as the final marshal below: keep the result
			// stable rather than panicking on an unmarshalable default.
			b = []byte(fmt.Sprintf("marshal-error:%v", err))
		}
		encoded[i] = b
	}
	order := make([]int, len(entities))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool {
		i, j := order[a], order[b]
		if entities[i].Name != entities[j].Name {
			return entities[i].Name < entities[j].Name
		}
		return bytes.Compare(encoded[i], encoded[j]) < 0
	})

	// Collapse versions that project to the same schema. The serving side
	// contributes one entry per registered version of a name while the
	// generation side has one declaration; without this, an app whose v1 and
	// v2 expose the same shape could never match its own manifest. Two
	// versions that genuinely differ survive as separate entries and still
	// signal drift, which is the case worth reporting.
	sorted := make([]hashEntity, 0, len(order))
	var prev []byte
	for _, idx := range order {
		if prev != nil && bytes.Equal(encoded[idx], prev) {
			continue
		}
		sorted = append(sorted, entities[idx])
		prev = encoded[idx]
	}
	entities = sorted

	raw, err := json.Marshal(entities)
	if err != nil {
		// Marshalling plain structs of strings/bools/ints cannot fail;
		// a non-nil error here means a default value snuck through with
		// an unmarshalable type, which the round-trip above already
		// filters. Hash the error text so the result is still stable.
		raw = []byte(fmt.Sprintf("marshal-error:%v", err))
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("sha256:%x", sum)
}

// RegistryNamedConfigs adapts a live entity registry to SchemaHash input,
// restricted to the given entity names (the manifest's Entities list).
//
// A name may resolve to MULTIPLE entities when it is registered under
// several API versions (e.g. /api/v1/posts and /api/v2/posts). Every
// version is included so the hash reflects the full live surface.
//
// The previous implementation called reg.Get(name), which returns an
// ambiguity error when a name has several versions and none is
// unversioned — and the error path silently skipped the entity, making a
// versioned entity look identical to a deleted one. That produced false
// drift: the live hash omitted the entity and never matched the manifest.
// Scanning AllSorted() and grouping by name avoids the ambiguity error
// entirely. Unknown names (no version registered) are still skipped — a
// genuinely deleted entity changes the hash by omission, which is the
// drift signal wanted.
func RegistryNamedConfigs(reg entity.Registry, names []string) []NamedConfig {
	byName := make(map[string][]*entity.Entity)
	for _, e := range reg.AllSorted() {
		byName[e.Config.Name] = append(byName[e.Config.Name], e)
	}
	var out []NamedConfig
	for _, name := range names {
		for _, e := range byName[name] {
			out = append(out, NamedConfig{
				Name:    e.Config.Name,
				Config:  e.Config,
				Version: e.Version,
			})
		}
	}
	return out
}
