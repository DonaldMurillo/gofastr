//go:build red

package config

import (
	"testing"
)

// CONTRACT-QUESTION red: the maintainer must decide whether a nested struct behind
// a POINTER is a supported config shape (recurse into it like a value struct) or an
// unsupported one (Load must error). Today it is neither: the field is silently
// skipped, its nested keys are never consulted, and Load returns nil.
// Family: F13 silent type confusion at the coercion boundary
// Property: every field of a config struct either binds from a source, takes its
// default, or makes Load fail — a struct field whose nested keys are silently
// unread is the exact "apps roll their own os.Getenv and misconfigure" bug class
// this package exists to remove (its own doc comment).
// Surfaces: core/config/config.go::bindStruct (recurses only on Kind()==Struct; a
// pointer-to-struct field falls through to the leaf path, where its uppercase-name
// key is almost never set, so the field is skipped with no error),
// core/config/config.go::LoadWith (no post-bind check that a bound struct still
// holds unread source keys).
// Finding: `DB *DBConfig` with DB_HOST/DB_PORT set in the source loads "successfully"
// with a nil DB and zero indication that those keys were ignored. The same shape
// written as a value struct binds every nested key, so the pointer spelling is a
// one-character divergence with a silent failure.
// Severity: medium — a misconfigured DSN/host silently stays at the zero value;
// nothing attacker-facing, but the package's whole contract is "no silent config".
// Fix direction: recurse into non-nil pointer-to-struct fields (allocating when a
// key is present), or return "field DB: unsupported type *DBConfig (use a value
// struct)" so the author learns at boot.

type redDBConfig struct {
	Host string `config:"HOST"`
	Port int    `config:"PORT"`
}

type redPointerCfg struct {
	Name string       `config:"NAME" default:"app"`
	DB   *redDBConfig `config:"db"`
}

type redValueCfg struct {
	Name string      `config:"NAME" default:"app"`
	DB   redDBConfig `config:"db"`
}

// TestPointerNestedStructBindsOrErrors: the pointer spelling must behave like the
// value spelling (bind DB_HOST) or fail loud; silently ignoring the nested keys is
// the only unacceptable outcome.
func TestPointerNestedStructBindsOrErrors(t *testing.T) {
	src := MapSource{
		"NAME":    "svc",
		"DB_HOST": "db.internal",
		"DB_PORT": "5432",
	}

	// Control: the value-struct spelling binds the nested keys today.
	var val redValueCfg
	if err := Load(&val, src); err != nil {
		t.Fatalf("value-struct Load: %v", err)
	}
	if val.DB.Host != "db.internal" || val.DB.Port != 5432 {
		t.Fatalf("control: value struct did not bind nested keys: %+v", val)
	}

	var ptr redPointerCfg
	if err := Load(&ptr, src); err != nil {
		// Erroring on the pointer shape is the second acceptable outcome.
		return
	}
	if ptr.DB == nil || ptr.DB.Host != "db.internal" || ptr.DB.Port != 5432 {
		t.Fatalf("CONTRACT-QUESTION [config] Load(&cfg{DB *DBConfig}) returned nil while silently ignoring "+
			"DB_HOST/DB_PORT (DB=%+v) — a nested struct behind a pointer is neither bound nor rejected; the "+
			"one-character divergence from the value-struct spelling (which binds both keys) turns into a nil "+
			"config with no error, the silent-misconfiguration class this package exists to prevent", ptr.DB)
	}
}
