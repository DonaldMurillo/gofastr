package upgrade_test

import "testing"

// configKit is the core/config surface the validate-tag note matches.
var configKit = map[string]string{"core/config/config.go": `package config

type Source interface{ Get(string) (string, bool) }
type MapSource map[string]string

func (m MapSource) Get(k string) (string, bool) { v, ok := m[k]; return v, ok }

func Load(cfg any, sources ...Source) error     { return nil }
func LoadWith(cfg any, sources ...Source) error { return nil }
func MustLoad(cfg any, sources ...Source)       {}
`}

// Each loader is found where an app calls it, and an app that only
// names a Source is quiet.
func TestV088ConfigNoteHitsLoaders(t *testing.T) {
	n := v088Note(t, "core/config.Load enforces validate: tags")
	if got := v088Hits(t, n, configKit, `package main

import "github.com/DonaldMurillo/gofastr/core/config"

type Cfg struct{ Port int }

func main() {
	var c Cfg
	_ = config.Load(&c)
	_ = config.LoadWith(&c)
	config.MustLoad(&c)
}
`); len(got) != 3 {
		t.Fatalf("hits = %v, want the Load, LoadWith and MustLoad calls", got)
	}
	if got := v088Hits(t, n, configKit, `package main

import "github.com/DonaldMurillo/gofastr/core/config"

func main() { var _ config.MapSource }
`); len(got) != 0 {
		t.Fatalf("fires with no loader call: %v", got)
	}
}
