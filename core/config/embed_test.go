package config

import (
	"strings"
	"testing"
)

// An embedded struct's fields are promoted the way encoding/json
// promotes them: `type Cfg struct{ Base }` reads Base's PORT as PORT,
// not BASE_PORT, and the old prefixed key is not read at all.

type embedBase struct {
	Port  int    `config:"PORT"`
	Token string `config:"TOKEN" sensitive:"true"`
}

func TestEmbeddedStructFieldsArePromoted(t *testing.T) {
	type cfg struct {
		embedBase
		Name string `config:"NAME"`
	}
	var c cfg
	if err := Load(&c, MapSource{"PORT": "9000", "TOKEN": "t", "NAME": "n"}); err != nil {
		t.Fatal(err)
	}
	if c.Port != 9000 || c.Token != "t" || c.Name != "n" {
		t.Fatalf("promoted fields not bound: %+v", c)
	}
}

func TestEmbeddedPrefixedKeyIsNotRead(t *testing.T) {
	type Base struct {
		Port int `config:"PORT"`
	}
	type cfg struct{ Base }
	var c cfg
	if err := Load(&c, MapSource{"BASE_PORT": "9000"}); err != nil {
		t.Fatal(err)
	}
	if c.Port != 0 {
		t.Fatalf("BASE_PORT bound Port = %d; the prefixed key has no fallback", c.Port)
	}
	if err := Load(&c, MapSource{"PORT": "9001"}); err != nil {
		t.Fatal(err)
	}
	if c.Port != 9001 {
		t.Fatalf("exported embed: Port = %d, want 9001 from PORT", c.Port)
	}
}

// An unexported embedded type promotes its exported fields, as
// encoding/json does; it used to be skipped because the embed itself
// cannot be set.
func TestUnexportedEmbedPromotes(t *testing.T) {
	type cfg struct{ embedBase }
	var c cfg
	if err := Load(&c, MapSource{"PORT": "7"}); err != nil {
		t.Fatal(err)
	}
	if c.Port != 7 {
		t.Fatalf("unexported embed: Port = %d, want 7", c.Port)
	}
}

type EmbedPtrBase struct {
	Port int    `config:"PORT"`
	Mode string `config:"MODE" default:"fast"`
}

// A pointer embed is allocated when one of its promoted keys is present,
// and then binds every promoted field (defaults included); it stays nil
// when none is.
func TestPointerEmbedAllocatedWhenKeyPresent(t *testing.T) {
	type cfg struct {
		*EmbedPtrBase
		Name string `config:"NAME"`
	}
	var absent cfg
	if err := Load(&absent, MapSource{"NAME": "x"}); err != nil {
		t.Fatal(err)
	}
	if absent.EmbedPtrBase != nil {
		t.Fatalf("pointer embed allocated with no key present: %+v", absent.EmbedPtrBase)
	}
	var present cfg
	if err := Load(&present, MapSource{"PORT": "8"}); err != nil {
		t.Fatal(err)
	}
	if present.EmbedPtrBase == nil || present.Port != 8 || present.Mode != "fast" {
		t.Fatalf("pointer embed not bound from PORT: %+v", present.EmbedPtrBase)
	}
	var prefixed cfg
	if err := Load(&prefixed, MapSource{"EMBEDPTRBASE_PORT": "8"}); err != nil {
		t.Fatal(err)
	}
	if prefixed.EmbedPtrBase != nil {
		t.Fatal("the old prefixed key allocated the pointer embed")
	}
}

type embedPtrHidden struct {
	Port int `config:"PORT"`
}

// A nil pointer to an unexported embedded type cannot be allocated
// through reflect. With a key present that is an error naming the
// field, never a silent skip; with none it stays nil. A pointer the
// host set binds through.
func TestUnexportedPointerEmbed(t *testing.T) {
	type cfg struct{ *embedPtrHidden }
	var absent cfg
	if err := Load(&absent, MapSource{}); err != nil {
		t.Fatalf("no key present: %v", err)
	}
	var present cfg
	err := Load(&present, MapSource{"PORT": "5"})
	if err == nil || !strings.Contains(err.Error(), "embedPtrHidden") {
		t.Fatalf("Load = %v, want an error naming the unexported pointer embed", err)
	}
	preset := cfg{&embedPtrHidden{}}
	if err := Load(&preset, MapSource{"PORT": "5"}); err != nil {
		t.Fatal(err)
	}
	if preset.Port != 5 {
		t.Fatalf("preset unexported pointer embed: Port = %d, want 5", preset.Port)
	}
}

// A shallower field shadows a promoted one with the same key: only the
// outer field is bound, and the shadowed field's tags do not apply.
func TestShallowerFieldWins(t *testing.T) {
	type Inner struct {
		Port int `config:"PORT" required:"true"`
	}
	type cfg struct {
		Inner
		Port int `config:"PORT" default:"80"`
	}
	var c cfg
	if err := Load(&c, MapSource{}); err != nil {
		t.Fatalf("shadowed required field enforced: %v", err)
	}
	if c.Port != 80 || c.Inner.Port != 0 {
		t.Fatalf("outer = %d inner = %d, want 80 and 0", c.Port, c.Inner.Port)
	}
	if err := Load(&c, MapSource{"PORT": "1"}); err != nil {
		t.Fatal(err)
	}
	if c.Port != 1 || c.Inner.Port != 0 {
		t.Fatalf("outer = %d inner = %d, want 1 and 0", c.Port, c.Inner.Port)
	}
}

// Two fields at the same depth reading one key are ambiguous. Load
// refuses the type, whatever the sources hold, instead of picking one.
func TestSameDepthKeyIsAmbiguous(t *testing.T) {
	type A struct {
		Port int `config:"PORT"`
	}
	type B struct {
		Listen int `config:"PORT"`
	}
	type cfg struct {
		A
		B
	}
	for _, src := range []MapSource{{}, {"PORT": "1"}} {
		var c cfg
		err := Load(&c, src)
		if err == nil {
			t.Fatalf("Load(%v) accepted two PORT fields at one depth: %+v", src, c)
		}
		for _, want := range []string{"PORT", "A.Port", "B.Listen", "ambiguous"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not name %q", err, want)
			}
		}
	}
	// The same embedded type reached twice at one depth is ambiguous too.
	type C struct{ A }
	type D struct{ A }
	type twice struct {
		C
		D
	}
	var c twice
	if err := Load(&c, MapSource{}); err == nil {
		t.Fatal("A promoted twice at depth 2 was accepted")
	}
	// A shallower field resolves the conflict, as in encoding/json.
	type resolved struct {
		A
		B
		Port int `config:"PORT"`
	}
	var r resolved
	if err := Load(&r, MapSource{"PORT": "3"}); err != nil || r.Port != 3 {
		t.Fatalf("outer field did not resolve the conflict: %v %+v", err, r)
	}
}

// A NAMED struct field keeps its prefix: `DB Base` reads DB_PORT.
func TestNamedStructFieldKeepsPrefix(t *testing.T) {
	type Base struct {
		Port int `config:"PORT"`
	}
	type cfg struct {
		DB Base
	}
	var c cfg
	if err := Load(&c, MapSource{"PORT": "1", "DB_PORT": "2"}); err != nil {
		t.Fatal(err)
	}
	if c.DB.Port != 2 {
		t.Fatalf("DB.Port = %d, want 2 from DB_PORT", c.DB.Port)
	}
	// An embed inside a named field promotes under that field's prefix,
	// and a named field promoted from an embed keeps its own prefix.
	type nested struct {
		DB struct{ Base }
	}
	var n nested
	if err := Load(&n, MapSource{"DB_PORT": "3", "DB_BASE_PORT": "4"}); err != nil {
		t.Fatal(err)
	}
	if n.DB.Port != 3 {
		t.Fatalf("DB.Port = %d, want 3 from DB_PORT", n.DB.Port)
	}
	type withDB struct {
		Cache Base
	}
	type outer struct{ withDB }
	var o outer
	if err := Load(&o, MapSource{"CACHE_PORT": "5"}); err != nil {
		t.Fatal(err)
	}
	if o.Cache.Port != 5 {
		t.Fatalf("Cache.Port = %d, want 5 from CACHE_PORT", o.Cache.Port)
	}
}

// The required error names the key Load read: the promoted one.
func TestRequiredPromotedFieldNamesKey(t *testing.T) {
	type Base struct {
		DBURL string `config:"DATABASE_URL" required:"true"`
	}
	type cfg struct{ Base }
	var c cfg
	err := Load(&c, MapSource{"BASE_DATABASE_URL": "x"})
	if err == nil {
		t.Fatal("missing promoted required key passed")
	}
	if !strings.Contains(err.Error(), "(DATABASE_URL)") || strings.Contains(err.Error(), "BASE_DATABASE_URL") {
		t.Fatalf("error %q should name DATABASE_URL", err)
	}
}

// A config tag on an embedded struct does not rename a prefix: Load
// refuses it so the field is named instead (a named field keeps the
// NAME_ prefix).
func TestTaggedEmbedRefused(t *testing.T) {
	type Base struct {
		Port int `config:"PORT"`
	}
	type cfg struct {
		Base `config:"BASE"`
	}
	var c cfg
	err := Load(&c, MapSource{"PORT": "1"})
	if err == nil || !strings.Contains(err.Error(), "Base") {
		t.Fatalf("Load = %v, want a refusal naming the tagged embed", err)
	}
}

type embedLoop struct {
	*embedLoop
	Port int
}

type EmbedLoop struct {
	*EmbedLoop
	Port int
}

// A recursive embed is refused, not walked forever.
func TestRecursiveEmbedRefused(t *testing.T) {
	var a embedLoop
	if Load(&a, MapSource{"PORT": "1"}) == nil {
		t.Fatal("unexported recursive embed accepted")
	}
	var b EmbedLoop
	if Load(&b, MapSource{"PORT": "1"}) == nil {
		t.Fatal("exported recursive embed accepted")
	}
}

type EmbedPtrNeeds struct {
	Need string `config:"NEED" required:"true"`
	Port int    `config:"PORT"`
}

// The pointer embed is allocated before any field binds, so a required
// field declared ahead of the present key is still enforced.
func TestPointerEmbedEnforcesEarlierRequired(t *testing.T) {
	type cfg struct{ *EmbedPtrNeeds }
	var c cfg
	err := Load(&c, MapSource{"PORT": "1"})
	if err == nil || !strings.Contains(err.Error(), "(NEED)") {
		t.Fatalf("Load = %v, want the required NEED refused", err)
	}
}
