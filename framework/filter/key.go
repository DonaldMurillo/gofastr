package filter

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// BracketOps lists the operator words the bracket form ?field[op]= takes,
// in the order a refusal names them. Each is its FilterOp's own spelling,
// the word its suffix carries after the underscore (eq has no suffix).
var BracketOps = [...]FilterOp{OpEq, OpNe, OpGt, OpGte, OpLt, OpLte, OpLike, OpIn}

// bracketOpList is BracketOps joined for the unknown-operator refusal.
var bracketOpList = func() string {
	words := make([]string, len(BracketOps))
	for i, op := range BracketOps {
		words[i] = string(op)
	}
	return strings.Join(words, ", ")
}()

// BracketKey spells the explicit form of a filter on name with op,
// name[op]. The text before the brackets is always the whole field (or
// wire) name, never cut at an operator suffix, so it reaches a field
// whose name ends in one: check_in[eq] is check_in = …, check[in] is
// check IN (…).
func BracketKey(name string, op FilterOp) string {
	return name + "[" + string(op) + "]"
}

// SuffixKey spells the suffix form of a filter on name with op: name
// alone for equality, name plus the op's suffix otherwise.
func SuffixKey(name string, op FilterOp) string {
	for _, s := range FilterSuffixes {
		if s.Op == op {
			return name + s.Suffix
		}
	}
	return name
}

// splitBracket reports whether key is in the bracket form name[op] and
// returns its two halves. The name is everything before the first '[';
// the op is everything between it and the closing ']' that ends the key.
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

// IsBracketKey reports whether key is in the bracket form name[op]
// (whatever the op word), the explicit spelling that never reads as a
// suffix or as a reserved control word.
func IsBracketKey(key string) bool {
	_, _, ok := splitBracket(key)
	return ok
}

// bracketOp resolves an operator word to its FilterOp.
func bracketOp(word string) (FilterOp, bool) {
	for _, op := range BracketOps {
		if string(op) == word {
			return op, true
		}
	}
	return "", false
}

// cutSuffix returns the name and op a bare key reads as when it ends in
// an operator suffix (at most one suffix in FilterSuffixes matches any
// key: none is a suffix of another), or ok false.
func cutSuffix(key string) (name string, op FilterOp, ok bool) {
	for _, s := range FilterSuffixes {
		if before, found := strings.CutSuffix(key, s.Suffix); found {
			return before, s.Op, true
		}
	}
	return "", "", false
}

// ReadKey reads one filter key against the names a target answers to.
// named reports whether a name is one a caller may put in a filter key:
// the column or wire name of a field that is not Hidden. NoQuery fields
// count (they are refused by name afterwards); Hidden ones never do, so
// a Hidden field and an absent one read alike.
//
// The bracket form name[op] names its field with the whole text before
// the brackets and its operator by word (BracketOps). A word outside
// that list is a refusal, but only once name is named: an unnamed name
// reads as an unknown field first, the answer an absent one gets.
//
// A bare key is equality on itself when it is a name, or op on the name
// before its operator suffix when that is a name. When both hold
// (fields check and check_in, key check_in) the key reads two ways and
// is refused, naming both readings and the bracket spellings that pick
// one: guessing either answered the other reading's rows with a 200.
//
// ok is false when no reading names a field. name and op are then the
// reading the caller most likely meant, for the unknown-field refusal:
// the bracket name, else the cut at the key's suffix, else the key.
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

// ambiguousKeyError is the refusal for a bare key that is a field's name
// and also another field's name plus an operator suffix.
func ambiguousKeyError(key, cut string, op FilterOp) error {
	return i18n.NewError(reasonAmbiguousFilter, map[string]any{
		"key":   strconv.Quote(key),
		"field": strconv.Quote(cut),
		"op":    string(op),
		"exact": strconv.Quote(BracketKey(key, OpEq)),
		"split": strconv.Quote(BracketKey(cut, op)),
	})
}

// IsAmbiguousKey reports whether the bare key reads two ways among the
// names named accepts: it is a name, and it is also another name plus an
// operator suffix. ReadKey refuses such a key; a client sends a bracket
// spelling instead.
func IsAmbiguousKey(key string, named func(string) bool) bool {
	if !named(key) {
		return false
	}
	cut, _, ok := cutSuffix(key)
	return ok && named(cut)
}

// KeySpeller spells the query key a client sends to filter one name with
// one operator, given every name a target answers to (column and wire
// names of its non-Hidden fields, NoQuery ones included). Generators and
// re-dispatching surfaces (the MCP list tool, the CLI, the OpenAPI
// document) use it so a field whose bare key is ambiguous is addressed
// by its bracket spelling, and every other key keeps its suffix
// spelling.
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

// SpellerFor is the KeySpeller over the names ParseFiltersValues reads
// for fields: each non-Hidden field's column name and WireName, NoQuery
// fields included (a NoQuery name still makes a bare key read two ways).
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

// Ambiguous reports whether the suffix spelling of name with op reads
// two ways (IsAmbiguousKey), so a client must send the bracket form.
func (s KeySpeller) Ambiguous(name string, op FilterOp) bool {
	return IsAmbiguousKey(SuffixKey(name, op), s.Named)
}

// AmbiguousKeys returns, sorted, every one of the speller's names whose
// bare key reads two ways: the keys a client must send in bracket form.
func (s KeySpeller) AmbiguousKeys() []string {
	var out []string
	for _, n := range slices.Sorted(maps.Keys(s.names)) {
		if IsAmbiguousKey(n, s.Named) {
			out = append(out, n)
		}
	}
	return out
}

// Key returns the key to send: the suffix spelling (SuffixKey) unless it
// is ambiguous, then the bracket spelling (BracketKey).
func (s KeySpeller) Key(name string, op FilterOp) string {
	if s.Ambiguous(name, op) {
		return BracketKey(name, op)
	}
	return SuffixKey(name, op)
}
