package appspec

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/codegen"
	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/framework"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

func decodeAppSpecEntities(node *coreyaml.Node) ([]framework.EntityDeclaration, error) {
	list, err := expectList(node, "entities")
	if err != nil {
		return nil, err
	}
	out := make([]framework.EntityDeclaration, 0, len(list))
	for i, item := range list {
		context := fmt.Sprintf("entities[%d]", i)
		m, err := expectMap(item, context)
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{
			"name": true, "table": true, "fields": true, "relations": true,
			"endpoints": true, "scope": true, "pagination": true, "exposure": true,
			"soft_delete": true, "multi_tenant": true, "tenant_field": true,
			"owner_field": true, "cross_owner_read": true, "search_fields": true,
			"access": true, "public": true, "timestamps": true, "crud": true,
			"mcp": true, "cursor_field": true, "cursor_fields": true,
			"max_list_limit": true, "indices": true, "properties": true,
			"renames": true, "read_scope": true,
			"display": true, "states": true, "primary_key": true,
		}
		if err := rejectUnknownKeys(m, allowed, context); err != nil {
			return nil, err
		}
		decl := framework.EntityDeclaration{
			Name:         stringValue(m["name"]),
			Table:        stringValue(m["table"]),
			SearchFields: stringListValue(m["search_fields"]),
			Properties:   mapValue(m["properties"]),
			Renames:      stringStringMapValue(m["renames"]),
		}
		if node := m["primary_key"]; node != nil {
			if node.Kind != coreyaml.List || len(node.List) == 0 {
				return nil, fmt.Errorf("%s.primary_key: want a non-empty list of field names", context)
			}
			decl.PrimaryKey = stringListValue(node)
		}
		if m["timestamps"] != nil {
			v := boolValue(m["timestamps"])
			decl.Timestamps = &v
		}

		scope, err := decodeEntityScope(m["scope"], context+".scope")
		if err != nil {
			return nil, err
		}
		decl.Scope, err = mergeEntityScopeDeclaration(decl.Name, m, scope, context)
		if err != nil {
			return nil, err
		}
		pagination, err := decodeEntityPagination(m["pagination"], context+".pagination")
		if err != nil {
			return nil, err
		}
		decl.Pagination, err = mergeEntityPaginationDeclaration(decl.Name, m, pagination, context)
		if err != nil {
			return nil, err
		}
		flatAccess, err := decodeEntityAccess(m["access"], context+".access")
		if err != nil {
			return nil, err
		}
		flatReadScope, err := decodeEntityReadScope(m["read_scope"], context+".read_scope")
		if err != nil {
			return nil, err
		}
		exposure, err := decodeEntityExposure(m["exposure"], context+".exposure")
		if err != nil {
			return nil, err
		}
		decl.Exposure, err = mergeEntityExposureDeclaration(decl.Name, m, exposure, flatAccess, flatReadScope, context)
		if err != nil {
			return nil, err
		}

		fields, err := decodeFields(m["fields"])
		if err != nil {
			return nil, err
		}
		decl.Fields = fields
		if decl.Scope != nil && decl.Scope.OwnerField != "" &&
			!appSpecEntityHasField(decl, decl.Scope.OwnerField) {
			decl.Fields = append(decl.Fields, framework.FieldDeclaration{
				Name:   decl.Scope.OwnerField,
				Type:   "string",
				Hidden: true,
			})
		}
		if decl.Scope != nil && decl.Scope.CrossOwnerRead != "" && decl.Scope.OwnerField == "" {
			return nil, fmt.Errorf(
				"app spec: entity %q sets cross_owner_read %q but has no owner_field (cross-owner read only applies to owner-scoped entities)",
				decl.Name, decl.Scope.CrossOwnerRead,
			)
		}
		for _, sf := range decl.SearchFields {
			var found *framework.FieldDeclaration
			for j := range decl.Fields {
				if decl.Fields[j].Name == sf {
					found = &decl.Fields[j]
					break
				}
			}
			if found == nil {
				return nil, fmt.Errorf("app spec: entity %q search_fields entry %q is not a declared field", decl.Name, sf)
			}
			if found.Hidden {
				return nil, fmt.Errorf("app spec: entity %q search_fields entry %q is Hidden (search would disclose its values)", decl.Name, sf)
			}
			if found.NoQuery {
				return nil, fmt.Errorf("app spec: entity %q search_fields entry %q is no_query (?q= would match on the stored value)", decl.Name, sf)
			}
			if found.Type != "string" && found.Type != "text" {
				return nil, fmt.Errorf("app spec: entity %q search_fields entry %q must be string or text, got %q", decl.Name, sf, found.Type)
			}
		}
		if rs := EntityDeclarationExposure(decl).ReadScope; rs != nil {
			for _, p := range rs.Filter {
				var found *framework.FieldDeclaration
				for j := range decl.Fields {
					if decl.Fields[j].Name == p.Field {
						found = &decl.Fields[j]
						break
					}
				}
				if found == nil {
					return nil, fmt.Errorf("app spec: entity %q read_scope field %q is not a declared field", decl.Name, p.Field)
				}
				if found.Hidden {
					return nil, fmt.Errorf("app spec: entity %q read_scope field %q is Hidden (a predicate on a masked column leaks its values through the row set)", decl.Name, p.Field)
				}
			}
		}
		var cursorFields []string
		if decl.Pagination != nil {
			cursorFields = append(cursorFields, decl.Pagination.CursorFields...)
			cursorFields = append(cursorFields, decl.Pagination.CursorField)
		}
		for _, cf := range cursorFields {
			if cf == "" {
				continue
			}
			found, ok := appSpecColumn(decl, cf)
			if !ok {
				return nil, fmt.Errorf("app spec: entity %q cursor field %q is not a declared field", decl.Name, cf)
			}
			if found == nil {
				continue
			}
			if found.Hidden {
				return nil, fmt.Errorf("app spec: entity %q cursor field %q is Hidden (it is not in the projection, so paging cannot read it)", decl.Name, cf)
			}
			if found.NoQuery {
				return nil, fmt.Errorf("app spec: entity %q cursor field %q is no_query (the cursor token would carry its stored value)", decl.Name, cf)
			}
			if strings.EqualFold(strings.TrimSpace(found.Type), "json") {
				return nil, fmt.Errorf("app spec: entity %q cursor field %q is json (keyset paging cannot compare a JSON document); page on a scalar column", decl.Name, cf)
			}
			isKey := slices.Contains(decl.PrimaryKey, cf) || (len(decl.PrimaryKey) == 0 && cf == "id")
			if !isKey && !found.Required && found.AutoGenerate == "" {
				return nil, fmt.Errorf("app spec: entity %q cursor field %q can hold NULL (keyset paging skips NULL rows); mark it required", decl.Name, cf)
			}
		}
		relations, err := decodeRelations(m["relations"])
		if err != nil {
			return nil, err
		}
		decl.Relations = relations
		indices, err := decodeIndices(m["indices"])
		if err != nil {
			return nil, err
		}
		decl.Indices = indices
		display, err := decodeDisplayDeclaration(m["display"], context+".display")
		if err != nil {
			return nil, err
		}
		decl.Display = display
		states, err := decodeStatesDeclaration(m["states"], context+".states")
		if err != nil {
			return nil, err
		}
		decl.States = states
		if m["endpoints"] != nil {
			return nil, fmt.Errorf("%s: an entity no longer lists endpoints; move each to the top-level endpoints: list with entity: %s and a handler link (entities/<file>.go#<Name>)", context, decl.Name)
		}
		out = append(out, decl)
	}
	return out, nil
}

func decodeStrictConfig[T any](node *coreyaml.Node, context string) (*T, error) {
	if node == nil {
		return nil, nil
	}
	data, err := json.Marshal(codegen.AnyValue(node))
	if err != nil {
		return nil, fmt.Errorf("app spec: %s: %w", context, err)
	}
	var cfg T
	if err := jsonexact.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("app spec: %s: %w", context, err)
	}
	return &cfg, nil
}

func decodeDisplayDeclaration(node *coreyaml.Node, context string) (*fwentity.DisplayConfig, error) {
	return decodeStrictConfig[fwentity.DisplayConfig](node, context)
}

func decodeStatesDeclaration(node *coreyaml.Node, context string) (*fwentity.StatesConfig, error) {
	return decodeStrictConfig[fwentity.StatesConfig](node, context)
}

func mergeEntityScopeDeclaration(
	name string,
	root map[string]*coreyaml.Node,
	grouped *fwentity.ScopeDeclaration,
	context string,
) (*fwentity.ScopeDeclaration, error) {
	groupMap, err := optionalEntityGroupMap(root["scope"], context+".scope")
	if err != nil {
		return nil, err
	}
	keys := []string{"soft_delete", "multi_tenant", "tenant_field", "owner_field", "cross_owner_read"}
	if grouped == nil && !hasEntityDeclarationKey(root, keys...) {
		return nil, nil
	}
	if grouped == nil {
		grouped = &fwentity.ScopeDeclaration{}
	}
	if node := root["soft_delete"]; node != nil {
		flat, err := protectiveBool(root, "soft_delete", context)
		if err != nil {
			return nil, err
		}
		if groupMap["soft_delete"] != nil && flat != grouped.SoftDelete {
			return nil, entityDeclarationConflict(name, "soft_delete", "scope.soft_delete", flat, grouped.SoftDelete)
		}
		grouped.SoftDelete = flat
	}
	if node := root["multi_tenant"]; node != nil {
		flat, err := protectiveBool(root, "multi_tenant", context)
		if err != nil {
			return nil, err
		}
		if groupMap["multi_tenant"] != nil && flat != grouped.MultiTenant {
			return nil, entityDeclarationConflict(name, "multi_tenant", "scope.multi_tenant", flat, grouped.MultiTenant)
		}
		grouped.MultiTenant = flat
	}
	for _, entry := range []struct {
		key string
		val *string
	}{
		{"tenant_field", &grouped.TenantField},
		{"owner_field", &grouped.OwnerField},
		{"cross_owner_read", &grouped.CrossOwnerRead},
	} {
		key, target := entry.key, entry.val
		if node := root[key]; node != nil {
			flat, err := requireScalarString(node, context, key)
			if err != nil {
				return nil, err
			}
			if groupMap[key] != nil && flat != *target {
				return nil, entityDeclarationConflict(name, key, "scope."+key, flat, *target)
			}
			*target = flat
		}
	}
	return grouped, nil
}

func mergeEntityPaginationDeclaration(
	name string,
	root map[string]*coreyaml.Node,
	grouped *fwentity.PaginationDeclaration,
	context string,
) (*fwentity.PaginationDeclaration, error) {
	groupMap, err := optionalEntityGroupMap(root["pagination"], context+".pagination")
	if err != nil {
		return nil, err
	}
	keys := []string{"cursor_field", "cursor_fields", "max_list_limit"}
	if grouped == nil && !hasEntityDeclarationKey(root, keys...) {
		return nil, nil
	}
	if grouped == nil {
		grouped = &fwentity.PaginationDeclaration{}
	}
	if node := root["cursor_field"]; node != nil {
		flat := stringValue(node)
		if groupMap["cursor_field"] != nil && flat != grouped.CursorField {
			return nil, entityDeclarationConflict(name, "cursor_field", "pagination.cursor_field", flat, grouped.CursorField)
		}
		grouped.CursorField = flat
	}
	if node := root["cursor_fields"]; node != nil {
		flat := stringListValue(node)
		if groupMap["cursor_fields"] != nil && !slices.Equal(flat, grouped.CursorFields) {
			return nil, entityDeclarationConflict(name, "cursor_fields", "pagination.cursor_fields", flat, grouped.CursorFields)
		}
		grouped.CursorFields = flat
	}
	if node := root["max_list_limit"]; node != nil {
		flat := intValue(node)
		if groupMap["max_list_limit"] != nil && flat != grouped.MaxListLimit {
			return nil, entityDeclarationConflict(name, "max_list_limit", "pagination.max_list_limit", flat, grouped.MaxListLimit)
		}
		grouped.MaxListLimit = flat
	}
	return grouped, nil
}

func mergeEntityExposureDeclaration(
	name string,
	root map[string]*coreyaml.Node,
	grouped *fwentity.ExposureDeclaration,
	flatAccess *fwentity.AccessDeclaration,
	flatReadScope *fwentity.ReadScopeDeclaration,
	context string,
) (*fwentity.ExposureDeclaration, error) {
	groupMap, err := optionalEntityGroupMap(root["exposure"], context+".exposure")
	if err != nil {
		return nil, err
	}
	keys := []string{"crud", "mcp", "public", "access", "read_scope"}
	if grouped == nil && !hasEntityDeclarationKey(root, keys...) {
		return nil, nil
	}
	if grouped == nil {
		grouped = &fwentity.ExposureDeclaration{}
	}
	if node := root["crud"]; node != nil {
		flat := boolValue(node)
		if groupMap["crud"] != nil && (grouped.CRUD == nil || flat != *grouped.CRUD) {
			return nil, entityDeclarationConflict(name, "crud", "exposure.crud", flat, grouped.CRUD)
		}
		grouped.CRUD = &flat
	}
	if node := root["mcp"]; node != nil {
		flat := boolValue(node)
		if groupMap["mcp"] != nil && flat != grouped.MCP {
			return nil, entityDeclarationConflict(name, "mcp", "exposure.mcp", flat, grouped.MCP)
		}
		grouped.MCP = flat
	}
	if node := root["public"]; node != nil {
		flat := boolValue(node)
		if groupMap["public"] != nil && flat != grouped.Public {
			return nil, entityDeclarationConflict(name, "public", "exposure.public", flat, grouped.Public)
		}
		grouped.Public = flat
	}
	if flatAccess != nil {
		if groupMap["access"] != nil && (grouped.Access == nil || *flatAccess != *grouped.Access) {
			return nil, entityDeclarationConflict(name, "access", "exposure.access", flatAccess, grouped.Access)
		}
		grouped.Access = flatAccess
	}
	if flatReadScope != nil {
		if groupMap["read_scope"] != nil && (grouped.ReadScope == nil || !reflect.DeepEqual(*flatReadScope, *grouped.ReadScope)) {
			return nil, entityDeclarationConflict(name, "read_scope", "exposure.read_scope", flatReadScope, grouped.ReadScope)
		}
		grouped.ReadScope = flatReadScope
	}
	return grouped, nil
}

func optionalEntityGroupMap(node *coreyaml.Node, context string) (map[string]*coreyaml.Node, error) {
	if node == nil {
		return map[string]*coreyaml.Node{}, nil
	}
	return expectMap(node, context)
}

func hasEntityDeclarationKey(root map[string]*coreyaml.Node, keys ...string) bool {
	for _, key := range keys {
		if root[key] != nil {
			return true
		}
	}
	return false
}

func entityDeclarationConflict(name, flatPath, groupedPath string, flat, grouped any) error {
	return fmt.Errorf(
		"app spec: entity %q has conflicting values for %s and %s (%v != %v)",
		name, flatPath, groupedPath, flat, grouped,
	)
}

func decodeEntityScope(node *coreyaml.Node, context string) (*fwentity.ScopeDeclaration, error) {
	if node == nil {
		return nil, nil
	}
	m, err := expectMap(node, context)
	if err != nil {
		return nil, err
	}
	if err := rejectUnknownKeys(m, map[string]bool{"soft_delete": true, "multi_tenant": true, "tenant_field": true, "owner_field": true, "cross_owner_read": true}, context); err != nil {
		return nil, err
	}
	softDelete, err := protectiveBool(m, "soft_delete", context)
	if err != nil {
		return nil, err
	}
	multiTenant, err := protectiveBool(m, "multi_tenant", context)
	if err != nil {
		return nil, err
	}
	tenantField, err := requireScalarString(m["tenant_field"], context, "tenant_field")
	if err != nil {
		return nil, err
	}
	ownerField, err := requireScalarString(m["owner_field"], context, "owner_field")
	if err != nil {
		return nil, err
	}
	crossOwnerRead, err := requireScalarString(m["cross_owner_read"], context, "cross_owner_read")
	if err != nil {
		return nil, err
	}
	return &fwentity.ScopeDeclaration{
		SoftDelete: softDelete, MultiTenant: multiTenant,
		TenantField: tenantField, OwnerField: ownerField,
		CrossOwnerRead: crossOwnerRead,
	}, nil
}

func decodeEntityPagination(node *coreyaml.Node, context string) (*fwentity.PaginationDeclaration, error) {
	if node == nil {
		return nil, nil
	}
	m, err := expectMap(node, context)
	if err != nil {
		return nil, err
	}
	if err := rejectUnknownKeys(m, map[string]bool{"cursor_field": true, "cursor_fields": true, "max_list_limit": true}, context); err != nil {
		return nil, err
	}
	return &fwentity.PaginationDeclaration{
		CursorField: stringValue(m["cursor_field"]), CursorFields: stringListValue(m["cursor_fields"]),
		MaxListLimit: intValue(m["max_list_limit"]),
	}, nil
}

func decodeEntityExposure(node *coreyaml.Node, context string) (*fwentity.ExposureDeclaration, error) {
	if node == nil {
		return nil, nil
	}
	m, err := expectMap(node, context)
	if err != nil {
		return nil, err
	}
	if err := rejectUnknownKeys(m, map[string]bool{"crud": true, "mcp": true, "public": true, "access": true, "read_scope": true}, context); err != nil {
		return nil, err
	}
	var crud *bool
	if m["crud"] != nil {
		v := boolValue(m["crud"])
		crud = &v
	}
	access, err := decodeEntityAccess(m["access"], context+".access")
	if err != nil {
		return nil, err
	}
	readScope, err := decodeEntityReadScope(m["read_scope"], context+".read_scope")
	if err != nil {
		return nil, err
	}
	return &fwentity.ExposureDeclaration{CRUD: crud, MCP: boolValue(m["mcp"]), Public: boolValue(m["public"]), Access: access, ReadScope: readScope}, nil
}

func decodeEntityAccess(node *coreyaml.Node, context string) (*fwentity.AccessDeclaration, error) {
	if node == nil {
		return nil, nil
	}
	m, err := expectMap(node, context)
	if err != nil {
		return nil, err
	}
	if err := rejectUnknownKeys(m, map[string]bool{"read": true, "create": true, "update": true, "delete": true}, context); err != nil {
		return nil, err
	}
	read, err := requireScalarString(m["read"], context, "read")
	if err != nil {
		return nil, err
	}
	create, err := requireScalarString(m["create"], context, "create")
	if err != nil {
		return nil, err
	}
	update, err := requireScalarString(m["update"], context, "update")
	if err != nil {
		return nil, err
	}
	del, err := requireScalarString(m["delete"], context, "delete")
	if err != nil {
		return nil, err
	}
	return &fwentity.AccessDeclaration{
		Read:   read,
		Create: create,
		Update: update,
		Delete: del,
	}, nil
}

func requireScalarString(node *coreyaml.Node, context, key string) (string, error) {
	if node == nil {
		return "", nil
	}
	if node.Kind == coreyaml.Map && node.Value == nil && len(node.Map) == 0 {
		return "", nil
	}
	if node.Kind != coreyaml.Scalar {
		return "", fmt.Errorf("%s: %q must be a single value, got a %s", context, key, yamlKindName(node.Kind))
	}
	if node.Value == nil {
		return "", nil
	}
	return fmt.Sprint(node.Value), nil
}

func yamlKindName(k coreyaml.Kind) string {
	switch k {
	case coreyaml.Scalar:
		return "value"
	case coreyaml.List:
		return "list"
	case coreyaml.Map:
		return "map"
	}
	return "node"
}

func decodeEntityReadScope(node *coreyaml.Node, context string) (*fwentity.ReadScopeDeclaration, error) {
	if node == nil {
		return nil, nil
	}
	m, err := expectMap(node, context)
	if err != nil {
		return nil, err
	}
	if err := rejectUnknownKeys(m, map[string]bool{"unrestricted": true, "filter": true}, context); err != nil {
		return nil, err
	}
	var filter []fwentity.RowPredicateDeclaration
	if fn := m["filter"]; fn != nil {
		list, err := expectList(fn, context+".filter")
		if err != nil {
			return nil, err
		}
		for i, item := range list {
			predContext := fmt.Sprintf("%s.filter[%d]", context, i)
			pm, err := expectMap(item, predContext)
			if err != nil {
				return nil, err
			}
			if err := rejectUnknownKeys(pm, map[string]bool{"field": true, "op": true, "value": true, "values": true}, predContext); err != nil {
				return nil, err
			}
			predField, err := requireScalarString(pm["field"], predContext, "field")
			if err != nil {
				return nil, err
			}
			predOp, err := requireScalarString(pm["op"], predContext, "op")
			if err != nil {
				return nil, err
			}
			predValue, err := requireScalarString(pm["value"], predContext, "value")
			if err != nil {
				return nil, err
			}
			pred := fwentity.RowPredicateDeclaration{
				Field:  predField,
				Op:     predOp,
				Value:  predValue,
				Values: stringListValue(pm["values"]),
			}
			if pred.Field == "" {
				return nil, fmt.Errorf("%s.field is required (a predicate with no column matches nothing)", predContext)
			}
			switch pred.Op {
			case "", "eq", "neq":
				if pred.Value == "" && len(pred.Values) == 0 {
					return nil, fmt.Errorf("%s: op %q needs a value (eq/neq compare one value; empty means eq)", predContext, pred.Op)
				}
				if len(pred.Values) > 0 {
					return nil, fmt.Errorf("%s: op %q must not set values (only in/not_in take a list)", predContext, pred.Op)
				}
			case "in", "not_in":
				if len(pred.Values) == 0 {
					return nil, fmt.Errorf("%s: op %q requires a non-empty values list", predContext, pred.Op)
				}
				if pred.Value != "" {
					return nil, fmt.Errorf("%s: op %q must not also set value (value and values are mutually exclusive)", predContext, pred.Op)
				}
			default:
				return nil, fmt.Errorf("%s.op is %q; must be one of eq, neq, in, not_in (empty means eq)", predContext, pred.Op)
			}
			filter = append(filter, pred)
		}
	}
	unrestricted, err := requireScalarString(m["unrestricted"], context, "unrestricted")
	if err != nil {
		return nil, err
	}
	if len(filter) == 0 && unrestricted == "" {
		return nil, fmt.Errorf("%s declares no filter and no unrestricted, so it filters nothing; remove the block or add a filter", context)
	}
	return &fwentity.ReadScopeDeclaration{
		Filter:       filter,
		Unrestricted: unrestricted,
	}, nil
}

func decodeIndices(node *coreyaml.Node) ([]framework.Index, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "indices")
	if err != nil {
		return nil, err
	}
	out := make([]framework.Index, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("indices[%d]", i))
		if err != nil {
			return nil, err
		}
		if err := rejectUnknownKeys(m, map[string]bool{"name": true, "columns": true, "unique": true}, fmt.Sprintf("indices[%d]", i)); err != nil {
			return nil, err
		}
		unique, err := protectiveBool(m, "unique", fmt.Sprintf("indices[%d]", i))
		if err != nil {
			return nil, err
		}
		out = append(out, framework.Index{
			Name:    stringValue(m["name"]),
			Columns: stringListValue(m["columns"]),
			Unique:  unique,
		})
	}
	return out, nil
}

func decodeFields(node *coreyaml.Node) ([]framework.FieldDeclaration, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "fields")
	if err != nil {
		return nil, err
	}
	out := make([]framework.FieldDeclaration, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("fields[%d]", i))
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{"name": true, "type": true, "required": true, "unique": true, "default": true, "auto_generate": true, "read_only": true, "hidden": true, "no_query": true, "max": true, "min": true, "pattern": true, "values": true, "to": true, "many": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("fields[%d]", i)); err != nil {
			return nil, err
		}
		fieldCtx := fmt.Sprintf("fields[%d]", i)
		required, err := protectiveBool(m, "required", fieldCtx)
		if err != nil {
			return nil, err
		}
		unique, err := protectiveBool(m, "unique", fieldCtx)
		if err != nil {
			return nil, err
		}
		readOnly, err := protectiveBool(m, "read_only", fieldCtx)
		if err != nil {
			return nil, err
		}
		hidden, err := protectiveBool(m, "hidden", fieldCtx)
		if err != nil {
			return nil, err
		}
		noQuery, err := protectiveBool(m, "no_query", fieldCtx)
		if err != nil {
			return nil, err
		}
		field := framework.FieldDeclaration{
			Name:         stringValue(m["name"]),
			Type:         stringValue(m["type"]),
			Required:     required,
			Unique:       unique,
			Default:      scalarValue(m["default"]),
			AutoGenerate: stringValue(m["auto_generate"]),
			ReadOnly:     readOnly,
			Hidden:       hidden,
			NoQuery:      noQuery,
			Pattern:      stringValue(m["pattern"]),
			Values:       stringListValue(m["values"]),
			To:           stringValue(m["to"]),
			Many:         boolValue(m["many"]),
		}
		if m["max"] != nil {
			v := floatValue(m["max"])
			field.Max = &v
		}
		if m["min"] != nil {
			v := floatValue(m["min"])
			field.Min = &v
		}
		out = append(out, field)
	}
	return out, nil
}

func decodeRelations(node *coreyaml.Node) ([]framework.Relation, error) {
	if node == nil {
		return nil, nil
	}
	list, err := expectList(node, "relations")
	if err != nil {
		return nil, err
	}
	out := make([]framework.Relation, 0, len(list))
	for i, item := range list {
		m, err := expectMap(item, fmt.Sprintf("relations[%d]", i))
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{"type": true, "name": true, "entity": true, "foreign_key": true, "through": true, "local_key": true, "foreign_key_target": true, "on_delete": true, "cascade_write": true, "hidden": true}
		if err := rejectUnknownKeys(m, allowed, fmt.Sprintf("relations[%d]", i)); err != nil {
			return nil, err
		}
		relType, err := relationTypeFromString(stringValue(m["type"]))
		if err != nil {
			return nil, err
		}
		var onDelete framework.OnDeleteAction
		if odVal := stringValue(m["on_delete"]); odVal != "" {
			switch strings.ToLower(odVal) {
			case "cascade":
				onDelete = framework.OnDeleteCascade
			case "set_null", "set null":
				onDelete = framework.OnDeleteSetNull
			case "restrict":
				onDelete = framework.OnDeleteRestrict
			case "no_action", "no action":
				onDelete = framework.OnDeleteNoAction
			default:
				return nil, fmt.Errorf("relations[%d]: unknown on_delete action %q", i, odVal)
			}
		}
		cascadeWrite := boolValue(m["cascade_write"])
		hidden, err := protectiveBool(m, "hidden", fmt.Sprintf("relations[%d]", i))
		if err != nil {
			return nil, err
		}
		var foreignKey []string
		if node := m["foreign_key"]; node != nil && node.Kind == coreyaml.List {
			foreignKey = stringListValue(node)
			if len(foreignKey) == 0 || slices.Contains(foreignKey, "") {
				return nil, fmt.Errorf("relations[%d].foreign_key: want a column name or a non-empty list of them", i)
			}
		} else if fk := stringValue(m["foreign_key"]); fk != "" {
			foreignKey = []string{fk}
		}
		out = append(out, framework.Relation{
			Type:             relType,
			Name:             stringValue(m["name"]),
			Entity:           stringValue(m["entity"]),
			ForeignKey:       foreignKey,
			Through:          stringValue(m["through"]),
			LocalKey:         stringValue(m["local_key"]),
			ForeignKeyTarget: stringValue(m["foreign_key_target"]),
			OnDelete:         onDelete,
			CascadeWrite:     cascadeWrite,
			Hidden:           hidden,
		})
	}
	return out, nil
}
