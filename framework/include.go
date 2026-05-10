package framework

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// parseIncludes reads the `?include=rel1,rel2` query parameter, validates each
// name against the entity's declared relations, and returns the matched
// Relations in input order. An unknown name yields an error so the caller can
// respond with 400 — silent ignoring would mask client typos.
func parseIncludes(r *http.Request, entity *Entity) ([]Relation, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("include"))
	if raw == "" {
		return nil, nil
	}
	relsByName := make(map[string]Relation, len(entity.Config.Relations))
	for _, rel := range entity.Config.Relations {
		relsByName[rel.Name] = rel
	}
	parts := strings.Split(raw, ",")
	out := make([]Relation, 0, len(parts))
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name == "" {
			continue
		}
		rel, ok := relsByName[name]
		if !ok {
			return nil, fmt.Errorf("unknown include %q", name)
		}
		out = append(out, rel)
	}
	return out, nil
}

// applyIncludes runs EagerLoad for the given relations and merges the results
// into rows under each relation's name (JSON-cased). Single-record relations
// (HasOne/BelongsTo) default to nil when missing; collection relations
// (HasMany/ManyToMany) default to an empty slice. The PK column is read from
// rows using the JSON-cased form of ch.PrimaryKey.
func (ch *CrudHandler) applyIncludes(ctx context.Context, rows []map[string]any, relations []Relation) error {
	if len(rows) == 0 || len(relations) == 0 {
		return nil
	}
	pkKey := ch.convertKey(ch.PrimaryKey)
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if v, ok := row[pkKey]; ok && v != nil {
			ids = append(ids, fmt.Sprintf("%v", v))
		}
	}

	loaded, err := EagerLoad(ctx, ch.DB, ch.Entity, relations, ids)
	if err != nil {
		return err
	}

	for _, row := range rows {
		idVal, ok := row[pkKey]
		if !ok || idVal == nil {
			continue
		}
		id := fmt.Sprintf("%v", idVal)
		bucket := loaded[id]
		for _, rel := range relations {
			outKey := ch.convertKey(rel.Name)
			val, present := bucket[rel.Name]
			row[outKey] = ch.formatRelationValue(rel, val, present)
		}
	}
	return nil
}

// formatRelationValue normalizes the EagerLoad output for a single relation
// into the JSON-cased shape the API exposes. Missing collection relations
// become an empty slice, missing single relations become nil.
func (ch *CrudHandler) formatRelationValue(rel Relation, val any, present bool) any {
	switch rel.Type {
	case RelHasMany, RelManyToMany:
		if !present {
			return []map[string]any{}
		}
		slice, ok := val.([]map[string]any)
		if !ok {
			return []map[string]any{}
		}
		out := make([]map[string]any, len(slice))
		for i, m := range slice {
			out[i] = ch.convertMapKeys(m)
		}
		return out
	default: // RelHasOne, RelManyToOne
		if !present {
			return nil
		}
		m, ok := val.(map[string]any)
		if !ok {
			return nil
		}
		return ch.convertMapKeys(m)
	}
}
