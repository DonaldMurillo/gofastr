package crud

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// Read-scope operators the filter package does not define.
const (
	readOpNeq   filter.FilterOp = "neq"
	readOpNotIn filter.FilterOp = "not_in"
)

// entityReadScope returns nil when ent has no Exposure block.
func entityReadScope(ent *entity.Entity) *entity.ReadScopeConfig {
	if ent == nil || ent.Config.Exposure == nil {
		return nil
	}
	return ent.Config.Exposure.ReadScope
}

// readScopeFilters is nil when the caller is unrestricted.
func readScopeFilters(ctx context.Context, ent *entity.Entity) []filter.ParsedFilter {
	rs := entityReadScope(ent)
	if rs == nil || len(rs.Filter) == 0 || readScopeUnrestricted(ctx, ent) {
		return nil
	}
	types := map[string]schema.FieldType{}
	for _, f := range ent.GetFields() {
		types[f.Name] = f.Type
	}
	var out []filter.ParsedFilter
	for pi, p := range rs.Filter {
		ft := types[p.Field]
		switch p.Op {
		case "", "eq":
			out = append(out, filter.ParsedFilter{Field: p.Field, Op: filter.OpEq, Value: p.Value}.Coerced(ft))
		case "neq":
			out = append(out, filter.ParsedFilter{Field: p.Field, Op: readOpNeq, Value: p.Value}.Coerced(ft))
		case "in":
			for _, v := range p.Values {
				out = append(out, filter.ParsedFilter{Field: p.Field, Op: filter.OpIn, Value: v}.Coerced(ft).InList("scope."+strconv.Itoa(pi)))
			}
		case "not_in":
			for _, v := range p.Values {
				out = append(out, filter.ParsedFilter{Field: p.Field, Op: readOpNotIn, Value: v}.Coerced(ft))
			}
		}
	}
	return out
}

func readScopeUnrestricted(ctx context.Context, ent *entity.Entity) bool {
	rs := entityReadScope(ent)
	if rs == nil {
		return true
	}
	if rs.Unrestricted != "" {
		return access.CanResource(ctx, access.Permission(rs.Unrestricted),
			access.Ref{Type: ent.GetName(), ID: ""})
	}
	_, signedIn := handler.GetUser(ctx)
	return signedIn
}

// renderReadScope renders preds as one parenthesised AND fragment, columns
// qualified by qualifier when set, placeholders numbered from startIdx.
func renderReadScope(preds []filter.ParsedFilter, qualifier string, startIdx int) (string, []any) {
	if len(preds) == 0 {
		return "", nil
	}
	if qualifier != "" && !isSafeTable(qualifier) {
		return "(1 = 0)", nil
	}
	for _, f := range preds {
		if !isSafeIdentifier(f.Field) {
			return "(1 = 0)", nil
		}
	}
	col := func(field string) string {
		if qualifier != "" {
			return query.QuoteKeyword(qualifier + "." + field)
		}
		return query.QuoteKeyword(field)
	}
	var parts []string
	var args []any
	idx := startIdx
	for i := 0; i < len(preds); i++ {
		f := preds[i]
		switch f.Op {
		case filter.OpIn:
			phs := []string{fmt.Sprintf("$%d", idx)}
			args = append(args, f.BindValue())
			idx++
			for i+1 < len(preds) && f.SameINList(preds[i+1]) {
				i++
				phs = append(phs, fmt.Sprintf("$%d", idx))
				args = append(args, preds[i].BindValue())
				idx++
			}
			parts = append(parts, fmt.Sprintf("%s IN (%s)", col(f.Field), strings.Join(phs, ", ")))
		case filter.OpEq:
			parts = append(parts, fmt.Sprintf("%s = $%d", col(f.Field), idx))
			args = append(args, f.BindValue())
			idx++
		case readOpNeq:
			parts = append(parts, fmt.Sprintf("%s <> $%d", col(f.Field), idx))
			args = append(args, f.BindValue())
			idx++
		case readOpNotIn:
			parts = append(parts, fmt.Sprintf("%s NOT IN ($%d)", col(f.Field), idx))
			args = append(args, f.BindValue())
			idx++
		default:
			parts = append(parts, "1 = 0")
		}
	}
	return "(" + strings.Join(parts, " AND ") + ")", args
}

func applyReadScopeWhere(addWhere func(sql string, args ...any), preds []filter.ParsedFilter) {
	if len(preds) == 0 {
		return
	}
	clause, args := renderReadScope(preds, "", 1)
	addWhere(clause, args...)
}

// ApplyReadScope ANDs the entity's ReadScope predicates for the request's
// caller into a SELECT on the entity's own table. It adds nothing when the
// entity declares none or the caller is unrestricted. Writes do not take it.
func (ch *CrudHandler) ApplyReadScope(qb *query.QueryBuilder, r *http.Request) {
	applyReadScopeWhere(func(sql string, args ...any) { qb.Where(sql, args...) }, readScopeFilters(r.Context(), ch.Entity))
}

// ApplyReadScopeCount mirrors ApplyReadScope for count queries.
func (ch *CrudHandler) ApplyReadScopeCount(cb *query.CountBuilder, r *http.Request) {
	applyReadScopeWhere(func(sql string, args ...any) { cb.Where(sql, args...) }, readScopeFilters(r.Context(), ch.Entity))
}

// readScopeAllowsRecord evaluates the read scope against an _events record.
func (ch *CrudHandler) readScopeAllowsRecord(record any) bool {
	rs := entityReadScope(ch.Entity)
	if rs == nil || len(rs.Filter) == 0 {
		return true
	}
	row, ok := record.(map[string]any)
	if !ok {
		return false
	}
	types := map[string]schema.FieldType{}
	for _, f := range ch.Entity.GetFields() {
		types[f.Name] = f.Type
	}
	for _, p := range rs.Filter {
		ft := types[p.Field]
		v, found := row[p.Field]
		if !found {
			v, found = row[ch.convertKey(p.Field)]
		}
		if !found {
			return false
		}
		if v == nil {
			return false
		}
		got := scopeText(ft, v)
		in := func() bool {
			return slices.ContainsFunc(p.Values, func(s string) bool { return scopeText(ft, s) == got })
		}
		switch p.Op {
		case "", "eq":
			if got != scopeText(ft, p.Value) {
				return false
			}
		case "neq":
			if got == scopeText(ft, p.Value) {
				return false
			}
		case "in":
			if !in() {
				return false
			}
		case "not_in":
			if in() {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// scopeText spells v the way readScopeAllowsRecord compares it.
func scopeText(ft schema.FieldType, v any) string {
	if ft == schema.Decimal {
		if r, ok := decimalRat(v); ok {
			return "\x00" + r.RatString()
		}
	}
	if s, ok := v.(string); ok {
		v = filter.TypedBind(ft, s)
	}
	switch x := v.(type) {
	case time.Time:
		if ft == schema.Timestamp {
			return schema.FormatTimestamp(x)
		}
	case int64:
		if ft == schema.Bool {
			return strconv.FormatBool(x != 0)
		}
	case float64:
		if ft == schema.Bool {
			return strconv.FormatBool(x != 0)
		}
	}
	return fmt.Sprint(v)
}

func decimalRat(v any) (*big.Rat, bool) {
	switch x := v.(type) {
	case string:
		if _, err := filter.CoerceValue("", schema.Decimal, x); err != nil {
			return nil, false
		}
		return new(big.Rat).SetString(x)
	case float64:
		return new(big.Rat).SetString(strconv.FormatFloat(x, 'g', -1, 64))
	case int64:
		return new(big.Rat).SetInt64(x), true
	}
	return nil, false
}
