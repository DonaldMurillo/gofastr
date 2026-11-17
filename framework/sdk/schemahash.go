package sdk

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

// NamedConfig pairs an entity name with its config, SchemaHash's input.
type NamedConfig struct {
	Name string
	// Version is the route-group prefix the entity is mounted at, unhashed.
	Version string
	Config  entity.EntityConfig
}

// SchemaHash returns a "sha256:<hex>" digest of the client-visible schema
// of named under jsonCase ("" is camel).
func SchemaHash(named []NamedConfig, jsonCase string) string {
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

		WireName string `json:"wireName,omitempty"`
	}
	type hashRelation struct {
		Type       int    `json:"type"`
		Name       string `json:"name"`
		Entity     string `json:"entity"`
		ForeignKey string `json:"foreignKey,omitempty"`

		ForeignKeys []string `json:"foreignKeys,omitempty"`
		Through     string   `json:"through,omitempty"`
	}
	type hashTransition struct {
		Key        string   `json:"key"`
		Label      string   `json:"label,omitempty"`
		From       []string `json:"from"`
		To         string   `json:"to"`
		Stamp      string   `json:"stamp,omitempty"`
		Variant    string   `json:"variant,omitempty"`
		Permission string   `json:"permission,omitempty"`
		System     bool     `json:"system,omitempty"`
	}
	type hashStates struct {
		Field       string           `json:"field"`
		Initial     []string         `json:"initial,omitempty"`
		Transitions []hashTransition `json:"transitions,omitempty"`
		Advisory    bool             `json:"advisory,omitempty"`
	}
	type hashParam struct {
		Name     string `json:"name"`
		Type     int    `json:"type"`
		Required bool   `json:"required"`
	}
	type hashEndpoint struct {
		Name    string      `json:"name"`
		Method  string      `json:"method"`
		Path    string      `json:"path"`
		Input   []hashParam `json:"input,omitempty"`
		Returns string      `json:"returns,omitempty"`
	}
	type hashEntity struct {
		Name       string `json:"name"`
		Table      string `json:"table"`
		Public     bool   `json:"public"`
		SoftDelete bool   `json:"softDelete"`

		PrimaryKey   []string       `json:"primaryKey"`
		SearchFields []string       `json:"searchFields,omitempty"`
		Fields       []hashField    `json:"fields"`
		Relations    []hashRelation `json:"relations,omitempty"`

		States *hashStates `json:"states,omitempty"`

		Endpoints []hashEndpoint `json:"endpoints,omitempty"`
	}

	entities := make([]hashEntity, 0, len(named))
	for _, n := range named {
		ent := entity.Define(n.Name, n.Config)
		cfg := ent.Config

		he := hashEntity{
			Name:       cfg.Name,
			Table:      cfg.Table,
			Public:     cfg.Exposure.Public,
			SoftDelete: cfg.Scope.SoftDelete,
			PrimaryKey: ent.KeyColumns(),
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

				NoQuery:  f.NoQuery,
				To:       f.To,
				Many:     f.Many,
				WireName: f.WireName,
			}
			hf.Values = append(hf.Values, f.Values...)
			if f.Default != nil {
				if raw, err := json.Marshal(f.Default); err == nil {
					var v any
					if err := jsonexact.Unmarshal(raw, &v); err != nil {
						panic(fmt.Sprintf("sdk: schema hash: default %T failed its own JSON round-trip: %v", f.Default, err))
					}
					hf.Default = v
				}
			}
			he.Fields = append(he.Fields, hf)
		}
		sort.Slice(he.Fields, func(i, j int) bool { return he.Fields[i].Name < he.Fields[j].Name })

		for _, r := range cfg.Relations {
			hr := hashRelation{Type: int(r.Type), Name: r.Name, Entity: r.Entity, Through: r.Through}
			if len(r.ForeignKey) == 1 {
				hr.ForeignKey = r.ForeignKey[0]
			} else {
				hr.ForeignKeys = r.ForeignKey
			}
			he.Relations = append(he.Relations, hr)
		}
		sort.Slice(he.Relations, func(i, j int) bool { return he.Relations[i].Name < he.Relations[j].Name })

		if cfg.States != nil {
			hs := hashStates{
				Field:       cfg.States.Field,
				Initial:     append([]string(nil), cfg.States.Initial...),
				Advisory:    cfg.States.Advisory,
				Transitions: make([]hashTransition, 0, len(cfg.States.Transitions)),
			}
			for _, t := range cfg.States.Transitions {
				hs.Transitions = append(hs.Transitions, hashTransition{
					Key:        t.Key,
					Label:      t.Label,
					From:       append([]string(nil), t.From...),
					To:         t.To,
					Stamp:      t.Stamp,
					Variant:    t.Variant,
					Permission: t.Permission,
					System:     t.System,
				})
			}
			he.States = &hs
		}

		for _, ep := range cfg.Endpoints {
			if !ep.SDK {
				continue
			}
			hp := hashEndpoint{Name: ep.Name, Method: ep.Method, Path: ep.Path, Returns: ep.Returns}
			for _, f := range ep.InputSchema {
				hp.Input = append(hp.Input, hashParam{Name: f.Name, Type: int(f.Type), Required: f.Required})
			}
			he.Endpoints = append(he.Endpoints, hp)
		}
		sort.Slice(he.Endpoints, func(i, j int) bool { return he.Endpoints[i].Name < he.Endpoints[j].Name })

		entities = append(entities, he)
	}

	encoded := make([][]byte, len(entities))
	for i, he := range entities {
		b, err := json.Marshal(he)
		if err != nil {
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
	if jsonCase == "" {
		jsonCase = "camelCase"
	}
	payload := struct {
		JSONCase string       `json:"jsonCase"`
		Entities []hashEntity `json:"entities"`
	}{JSONCase: jsonCase, Entities: entities}
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(fmt.Sprintf("marshal-error:%v", err))
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("sha256:%x", sum)
}

// RegistryNamedConfigs returns SchemaHash input for every version of names.
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
