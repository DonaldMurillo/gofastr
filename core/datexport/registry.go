// Package datexport registers the raw tables and stores that export, import and erasure reach.
package datexport

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// DataExporter declares one raw table for export; PrimaryKey may be comma-joined or empty.
type DataExporter struct {
	Name       string
	Source     string
	Table      string
	PrimaryKey string
	Columns    []string
}

// EraseMode selects how a DataEraser removes rows: delete, anonymize or remove a member.
type EraseMode int

const (
	EraseDelete EraseMode = iota
	EraseAnonymize
	EraseRemoveMember
)

// IdentityKind selects the principal an eraser matches, resolved by a [DataIdentityResolver].
type IdentityKind int

const (
	IdentityUserID IdentityKind = iota
	IdentityEmail
)

// DataEraser declares how an erasure reaches the rows of one raw table.
type DataEraser struct {
	Name            string
	Source          string
	Table           string
	Column          string
	Mode            EraseMode
	ScrubColumns    []string
	Tombstone       string
	Identity        IdentityKind
	ValuePrefix     string
	Where           []Equals
	MemberSeparator string
}

// Equals is one column = value condition of DataEraser.Where.
type Equals struct {
	Column string
	Value  string
}

// DataIdentityResolver reads ValueColumn from Table where IDColumn is the erased user's id.
type DataIdentityResolver struct {
	Table        string
	IDColumn     string
	ValueColumn  string
	Canonicalize func(string) string
}

var (
	mu        sync.RWMutex
	entries   = []*DataExporter{}
	erasers   = []*DataEraser{}
	resolvers = map[IdentityKind]DataIdentityResolver{}
)

// Register adds or replaces e by Name, panicking on a Name outside [A-Za-z0-9_-].
func Register(e DataExporter) {
	if !validExporterName(e.Name) {
		panic(fmt.Sprintf("datexport: Register: unsafe exporter Name %q: "+
			"must be one path segment of [A-Za-z0-9_-] (it becomes the "+
			"<name>.ndjson path stem at export and import)", e.Name))
	}
	e = e.clone()
	for _, column := range strings.Split(e.PrimaryKey, ",") {
		column = strings.TrimSpace(column)
		if column != "" && !slices.Contains(e.Columns, column) {
			e.Columns = append(e.Columns, column)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	for i, ex := range entries {
		if ex.Name == e.Name {
			entries[i] = &e
			return
		}
	}
	entries = append(entries, &e)
}

func (e DataExporter) clone() DataExporter {
	e.Columns = slices.Clone(e.Columns)
	return e
}

// ScopedName returns the registration Name for a store of kind base built on table.
func ScopedName(base, table string) string {
	table = asciicase.Lower(table)
	b := []byte(base)
	if table != asciicase.Lower(base) {
		b = append(b, '-')
		for i := 0; i < len(table); i++ {
			c := table[i]
			switch {
			case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_':
				b = append(b, c)
			default:
				b = append(b, '-', hexDigits[c>>4], hexDigits[c&0xf])
			}
		}
	}
	if len(b) > maxScopedName {
		sum := sha256.Sum256([]byte(table))
		b = append(b[:maxScopedName-17], '-')
		b = hex.AppendEncode(b, sum[:8])
	}
	return string(b)
}

const maxScopedName = 200

const hexDigits = "0123456789abcdef"

func validExporterName(n string) bool {
	if n == "" {
		return false
	}
	for _, r := range n {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// Unregister removes an exporter by Name and reports whether one was.
func Unregister(name string) bool {
	mu.Lock()
	defer mu.Unlock()
	for i, ex := range entries {
		if ex.Name == name {
			entries = append(entries[:i], entries[i+1:]...)
			return true
		}
	}
	return false
}

// All returns a copy of the registered exporters sorted by Name.
func All() []DataExporter {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]DataExporter, 0, len(entries))
	for _, ex := range entries {
		out = append(out, ex.clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RegisterEraser adds e, replacing an eraser of the same Name.
func RegisterEraser(e DataEraser) {
	mu.Lock()
	defer mu.Unlock()
	for i, ex := range erasers {
		if ex.Name == e.Name {
			erasers[i] = cloneEraser(e)
			return
		}
	}
	erasers = append(erasers, cloneEraser(e))
}

func cloneEraser(e DataEraser) *DataEraser {
	e.ScrubColumns = slices.Clone(e.ScrubColumns)
	e.Where = slices.Clone(e.Where)
	return &e
}

// UnregisterEraser removes an eraser by Name and reports whether one was.
func UnregisterEraser(name string) bool {
	mu.Lock()
	defer mu.Unlock()
	for i, ex := range erasers {
		if ex.Name == name {
			erasers = append(erasers[:i], erasers[i+1:]...)
			return true
		}
	}
	return false
}

// AllErasers returns a copy of the registered erasers sorted by Name.
func AllErasers() []DataEraser {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]DataEraser, 0, len(erasers))
	for _, ex := range erasers {
		out = append(out, *cloneEraser(*ex))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RegisterIdentityResolver sets the resolver for kind, replacing any.
func RegisterIdentityResolver(kind IdentityKind, r DataIdentityResolver) {
	mu.Lock()
	defer mu.Unlock()
	resolvers[kind] = r
}

// UnregisterIdentityResolver removes kind's resolver and reports whether one was.
func UnregisterIdentityResolver(kind IdentityKind) bool {
	mu.Lock()
	defer mu.Unlock()
	_, ok := resolvers[kind]
	delete(resolvers, kind)
	return ok
}

// ResolveIdentity returns kind's resolver, false when none is registered.
func ResolveIdentity(kind IdentityKind) (DataIdentityResolver, bool) {
	mu.RLock()
	defer mu.RUnlock()
	r, ok := resolvers[kind]
	return r, ok
}

// AllIdentityResolvers returns a copy of the registered resolvers.
func AllIdentityResolvers() map[IdentityKind]DataIdentityResolver {
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[IdentityKind]DataIdentityResolver, len(resolvers))
	maps.Copy(out, resolvers)
	return out
}

// Reset clears every registry; it is for tests.
func Reset(_ *testing.T) {
	mu.Lock()
	defer mu.Unlock()
	entries = nil
	erasers = nil
	storeErasers = nil
	for k := range resolvers {
		delete(resolvers, k)
	}
}
