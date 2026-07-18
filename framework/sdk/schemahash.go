package sdk

import (
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
	Name   string
	Config entity.EntityConfig
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
		Values   []string `json:"values,omitempty"`
		Default  any      `json:"default,omitempty"`
		To       string   `json:"to,omitempty"`
		Many     bool     `json:"many,omitempty"`
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
			Public:     cfg.Public,
			SoftDelete: cfg.SoftDelete,
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
				To:       f.To,
				Many:     f.Many,
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
	sort.Slice(entities, func(i, j int) bool { return entities[i].Name < entities[j].Name })

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
// Unknown names are skipped — an entity that was deleted since generation
// changes the hash by omission, which is exactly the drift signal wanted.
func RegistryNamedConfigs(reg entity.Registry, names []string) []NamedConfig {
	var out []NamedConfig
	for _, name := range names {
		e, err := reg.Get(name)
		if err != nil || e == nil {
			continue
		}
		out = append(out, NamedConfig{Name: e.Config.Name, Config: e.Config})
	}
	return out
}
