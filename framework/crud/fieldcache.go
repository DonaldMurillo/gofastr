package crud

import (
	"slices"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/casing"
)

// fieldCacheData is one derived-field snapshot, never written after publish.
type fieldCacheData struct {
	fields           []schema.Field
	visible          []string
	visibleJSONs     []string
	wireKeyOf        map[string]string
	columnOfWire     map[string]string
	columnOfCamel    map[string]string
	columnOfSnake    map[string]string
	jsonColumns      map[string]struct{}
	jsonWireKeys     map[string]struct{}
	decimalWireKeys  map[string]struct{}
	dateWireKeys     map[string]struct{}
	dateColumns      map[string]struct{}
	timestampColumns map[string]struct{}
	boolColumns      map[string]struct{}
	jsonCase         JSONCase
	sig              uint64
}

// fieldCache is held by pointer so inTx's handler copies share it.
type fieldCache struct {
	mu  sync.RWMutex
	cur *fieldCacheData
}

func (fc *fieldCache) load() *fieldCacheData {
	fc.mu.RLock()
	s := fc.cur
	fc.mu.RUnlock()
	return s
}

func (fc *fieldCache) publish(s *fieldCacheData) {
	fc.mu.Lock()
	fc.cur = s
	fc.mu.Unlock()
}

func newFieldCache(ch *CrudHandler) *fieldCache {
	fc := &fieldCache{}
	fc.publish(buildFieldSnapshot(ch))
	return fc
}

var fieldCacheInitMu sync.Mutex

// snapshot builds the cache lazily for struct-literal handlers, and rebuilds it when JSONCase was assigned after construction.
func (ch *CrudHandler) snapshot() *fieldCacheData {
	if fc := ch.fcache; fc != nil {
		if s := fc.load(); s != nil && s.jsonCase == ch.JSONCase {
			return s
		}
	}
	fieldCacheInitMu.Lock()
	defer fieldCacheInitMu.Unlock()
	if ch.fcache == nil {
		ch.fcache = newFieldCache(ch)
	}
	if s := ch.fcache.load(); s == nil || s.jsonCase != ch.JSONCase {
		ch.fcache.publish(buildFieldSnapshot(ch))
	}
	return ch.fcache.load()
}

// refreshFieldCacheIfStale is for single-threaded host reads (VisibleFields), never the request path.
func (ch *CrudHandler) refreshFieldCacheIfStale() {
	if ch.fcache == nil {
		return
	}
	if s := ch.fcache.load(); s != nil && s.sig == ch.fieldCacheSignature() {
		return
	}
	ch.fcache.publish(buildFieldSnapshot(ch))
}

func buildFieldSnapshot(ch *CrudHandler) *fieldCacheData {
	s := &fieldCacheData{
		wireKeyOf:        map[string]string{},
		columnOfWire:     map[string]string{},
		columnOfCamel:    map[string]string{},
		columnOfSnake:    map[string]string{},
		jsonColumns:      map[string]struct{}{},
		jsonWireKeys:     map[string]struct{}{},
		decimalWireKeys:  map[string]struct{}{},
		dateWireKeys:     map[string]struct{}{},
		dateColumns:      map[string]struct{}{},
		timestampColumns: map[string]struct{}{},
		boolColumns:      map[string]struct{}{},
	}
	s.jsonCase = ch.JSONCase
	if ch.Entity == nil {
		s.sig = ch.fieldCacheSignature()
		return s
	}
	fields := ch.Entity.GetFields()
	s.fields = append([]schema.Field(nil), fields...)
	for _, f := range fields {
		wk := ch.wireKeyOfField(f)
		s.wireKeyOf[f.Name] = wk
		if _, exists := s.columnOfWire[wk]; !exists {
			s.columnOfWire[wk] = f.Name
		}
		for _, key := range []string{casing.ToCamel(f.Name), casing.ToCamel(wk)} {
			if prev, exists := s.columnOfCamel[key]; exists && prev != f.Name {
				s.columnOfCamel[key] = ""
				continue
			}
			s.columnOfCamel[key] = f.Name
		}
		for _, key := range []string{casing.ToSnake(f.Name), casing.ToSnake(wk)} {
			if prev, exists := s.columnOfSnake[key]; exists && prev != f.Name {
				s.columnOfSnake[key] = ""
				continue
			}
			s.columnOfSnake[key] = f.Name
		}
		if f.Type == schema.JSON {
			s.jsonColumns[f.Name] = struct{}{}
			s.jsonWireKeys[wk] = struct{}{}
		}
		if f.Type == schema.Decimal {
			s.decimalWireKeys[wk] = struct{}{}
		}
		if f.Type == schema.Date {
			s.dateWireKeys[wk] = struct{}{}
			s.dateColumns[f.Name] = struct{}{}
		}
		if f.Type == schema.Timestamp {
			s.timestampColumns[f.Name] = struct{}{}
		}
		if f.Type == schema.Bool {
			s.boolColumns[f.Name] = struct{}{}
		}
		if !f.Hidden {
			s.visible = append(s.visible, f.Name)
		}
	}
	s.visibleJSONs = convertedKeys(s.visible, func(col string) string { return s.wireKeyOf[col] })
	s.sig = ch.fieldCacheSignature()
	return s
}

func (ch *CrudHandler) snapshotFields() []schema.Field {
	return ch.snapshot().fields
}

// queryFields types a Relation to an integer key as schema.Int, per call because Registry is set after NewCrudHandler.
func (ch *CrudHandler) queryFields() []schema.Field {
	fields := ch.snapshot().fields
	if ch.Registry == nil || ch.Entity == nil {
		return fields
	}
	out, cloned := fields, false
	for i, f := range fields {
		if f.Type != schema.Relation || f.RawType != "" || !entity.RelationHoldsInt(ch.Registry, ch.Entity, f.Name) {
			continue
		}
		if !cloned {
			out, cloned = slices.Clone(fields), true
		}
		out[i].Type = schema.Int
	}
	return out
}

func (ch *CrudHandler) queryField(name string) schema.Field {
	for _, f := range ch.queryFields() {
		if f.Name == name {
			return f
		}
	}
	return schema.Field{Name: name}
}
