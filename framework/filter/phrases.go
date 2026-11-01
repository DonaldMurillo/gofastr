package filter

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals a filter, sort or where parse returns. Each error's
// Error() is the English below; an error writer translates it.
var (
	reasonInListOverCap       = i18n.Define("filter.in_list_over_cap", "in-list on {{field}} has {{count}} entries (max {{max}})")
	reasonNotFilterable       = i18n.Define("filter.not_filterable", "field {{field}} cannot be filtered")
	reasonNotSortable         = i18n.Define("filter.not_sortable", "field {{field}} cannot be sorted on")
	reasonUnknownFilterHint   = i18n.Define("filter.unknown_filter_hint", "unknown filter {{key}} (did you mean {{suggestion}}?)")
	reasonUnknownFilter       = i18n.Define("filter.unknown_filter", "unknown filter {{key}}")
	reasonAmbiguousFilter     = i18n.Define("filter.ambiguous_filter", "filter {{key}} reads two ways: equality on field {{key}}, or {{op}} on field {{field}}; send {{exact}} or {{split}} instead")
	reasonUnknownBracketOp    = i18n.Define("filter.unknown_bracket_op", "filter {{key}} names unknown operator {{op}}; use one of {{ops}}")
	reasonRepeatedFilter      = i18n.Define("filter.repeated_filter", "filter {{key}} is sent {{count}} times; send it once (only an _in filter takes several values)")
	reasonTooManySorts        = i18n.Define("filter.too_many_sorts", "too many sort fields: {{count}} (max {{max}})")
	reasonSortControlBytes    = i18n.Define("filter.sort_control_bytes", "invalid sort {{sort}}: control bytes not allowed")
	reasonInvalidSortField    = i18n.Define("filter.invalid_sort_field", "invalid sort field {{field}}")
	reasonOpTypeMismatch      = i18n.Define("filter.op_type_mismatch", "operator {{op}} cannot filter {{field}} (type {{type}})")
	reasonValueType           = i18n.Define("filter.value_type", "filter value for {{field}} is not a valid {{type}}")
	reasonWhereInvalidJSON    = i18n.Define("filter.where_invalid_json", "where: invalid JSON: {{detail}}")
	reasonWhereTooDeep        = i18n.Define("filter.where_too_deep", "where: nesting exceeds max depth {{max}}")
	reasonWhereTooManyNodes   = i18n.Define("filter.where_too_many_nodes", "where: exceeds max node count {{max}}")
	reasonWhereInvalidNode    = i18n.Define("filter.where_invalid_node", "where: invalid node: {{detail}}")
	reasonWhereNodeShape      = i18n.Define("filter.where_node_shape", "where: each node must be exactly one of and/or/field")
	reasonWhereUnknownOp      = i18n.Define("filter.where_unknown_operator", "where: unknown operator {{op}}")
	reasonWhereInOverCap      = i18n.Define("filter.where_in_over_cap", "where: in-list exceeds {{max}} entries")
	reasonWhereNoField        = i18n.Define("filter.where_no_field", "where: predicate leaf has no field")
	reasonWhereNotFilterable  = i18n.Define("filter.where_not_filterable", "where: field {{field}} cannot be filtered")
	reasonWhereUnknownField   = i18n.Define("filter.where_unknown_field", "where: unknown filter field {{field}}")
	reasonWhereOpTypeMismatch = i18n.Define("filter.where_op_type_mismatch", "where: operator {{op}} cannot filter {{field}} (type {{type}})")
	reasonWhereStrayMember    = i18n.Define("filter.where_stray_member", "where: op {{op}} does not read member {{member}}")
	reasonWhereInNeedsValues  = i18n.Define("filter.where_in_needs_values", "where: {{field}} with op in requires values")
)
