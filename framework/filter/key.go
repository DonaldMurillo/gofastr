package filter

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// BracketOps lists the operator words ?field[op]= takes, in the order a
// refusal names them.
var BracketOps = [...]FilterOp{OpEq, OpNe, OpGt, OpGte, OpLt, OpLte, OpLike, OpIn}

var bracketOpList = func() string {
	words := make([]string, len(BracketOps))
	for i, op := range BracketOps {
		words[i] = string(op)
	}
	return strings.Join(words, ", ")
}()

// BracketKey spells the bracket form name[op], which names the whole
// field before the brackets.
func BracketKey(name string, op FilterOp) string {
	return name + "[" + string(op) + "]"
}

// SuffixKey spells name alone for equality, name plus op's suffix otherwise.
func SuffixKey(name string, op FilterOp) string {
	for _, s := range FilterSuffixes {
		if s.Op == op {
			return name + s.Suffix
		}
	}
	return name
}

func splitBracket(key string) (name, op string, ok bool) {
	if !strings.HasSuffix(key, "]") {
		return "", "", false
	}
	i := strings.IndexByte(key, '[')
	if i < 0 {
		return "", "", false
	}
	return key[:i], key[i+1 : len(key)-1], true
}

// IsBracketKey reports whether key is in the form name[op], whatever op.
func IsBracketKey(key string) bool {
	_, _, ok := splitBracket(key)
	return ok
}

func bracketOp(word string) (FilterOp, bool) {
	for _, op := range BracketOps {
		if string(op) == word {
			return op, true
		}
	}
	return "", false
}

func cutSuffix(key string) (name string, op FilterOp, ok bool) {
	for _, s := range FilterSuffixes {
		if before, found := strings.CutSuffix(key, s.Suffix); found {
			return before, s.Op, true
		}
	}
	return "", "", false
}

// ReadKey reads a filter key against the names named accepts, erroring
// on a bare key that reads two ways or an unknown bracket op. ok is false
// when no reading names a field.
func ReadKey(key string, named func(string) bool) (name string, op FilterOp, ok bool, err error) {
	if base, word, bracket := splitBracket(key); bracket {
		if !named(base) {
			return base, OpEq, false, nil
		}
		op, known := bracketOp(word)
		if !known {
			return base, OpEq, true, i18n.NewError(reasonUnknownBracketOp, map[string]any{
				"key": strconv.Quote(key), "op": strconv.Quote(word), "ops": bracketOpList,
			})
		}
		return base, op, true, nil
	}
	cut, cutOp, suffixed := cutSuffix(key)
	exact := named(key)
	if suffixed && named(cut) {
		if exact {
			return key, OpEq, true, ambiguousKeyError(key, cut, cutOp)
		}
		return cut, cutOp, true, nil
	}
	if exact {
		return key, OpEq, true, nil
	}
	if suffixed {
		return cut, cutOp, false, nil
	}
	return key, OpEq, false, nil
}

func ambiguousKeyError(key, cut string, op FilterOp) error {
	return i18n.NewError(reasonAmbiguousFilter, map[string]any{
		"key":   strconv.Quote(key),
		"field": strconv.Quote(cut),
		"op":    string(op),
		"exact": strconv.Quote(BracketKey(key, OpEq)),
		"split": strconv.Quote(BracketKey(cut, op)),
	})
}

// IsAmbiguousKey reports whether the bare key is a name and also another
// name plus an operator suffix, the key ReadKey refuses.
func IsAmbiguousKey(key string, named func(string) bool) bool {
	if !named(key) {
		return false
	}
	cut, _, ok := cutSuffix(key)
	return ok && named(cut)
}

// KeySpeller spells the key a client sends to filter a name with an op,
// in bracket form where the suffix form is ambiguous.
type KeySpeller struct {
	names map[string]bool
}

// NewKeySpeller builds a KeySpeller over names. Empty names are ignored.
func NewKeySpeller(names ...string) KeySpeller {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		if n != "" {
			m[n] = true
		}
	}
	return KeySpeller{names: m}
}

// SpellerFor is the KeySpeller over the names ParseFiltersValues reads:
// each non-Hidden field's column and wire name, NoQuery ones included.
func SpellerFor(fields []schema.Field) KeySpeller {
	names := make([]string, 0, 2*len(fields))
	for _, f := range fields {
		if f.Hidden {
			continue
		}
		names = append(names, f.Name, f.WireName)
	}
	return NewKeySpeller(names...)
}

// Named reports whether name is one of the speller's names.
func (s KeySpeller) Named(name string) bool { return s.names[name] }

// Ambiguous reports whether SuffixKey(name, op) reads two ways.
func (s KeySpeller) Ambiguous(name string, op FilterOp) bool {
	return IsAmbiguousKey(SuffixKey(name, op), s.Named)
}

// AmbiguousKeys returns, sorted, the names whose bare key reads two ways.
func (s KeySpeller) AmbiguousKeys() []string {
	var out []string
	for _, n := range slices.Sorted(maps.Keys(s.names)) {
		if IsAmbiguousKey(n, s.Named) {
			out = append(out, n)
		}
	}
	return out
}

// Key returns SuffixKey, or BracketKey where that is ambiguous.
func (s KeySpeller) Key(name string, op FilterOp) string {
	if s.Ambiguous(name, op) {
		return BracketKey(name, op)
	}
	return SuffixKey(name, op)
}
