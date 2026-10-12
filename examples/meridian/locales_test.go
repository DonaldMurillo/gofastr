package main

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// flatCatalog reads a locale file as dotted key → text.
func flatCatalog(t *testing.T, name string) map[string]string {
	t.Helper()
	raw, err := localeFS.ReadFile("locales/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	var walk func(prefix string, node map[string]any)
	walk = func(prefix string, node map[string]any) {
		for k, v := range node {
			switch v := v.(type) {
			case string:
				out[prefix+k] = v
			case map[string]any:
				walk(prefix+k+".", v)
			default:
				t.Fatalf("%s%s is neither text nor a group", prefix, k)
			}
		}
	}
	walk("", doc)
	return out
}

var (
	bracePlaceholder = regexp.MustCompile(`\{\w+\}`)
	printfVerb       = regexp.MustCompile(`%[sd]`)
)

// The Spanish catalog names only keys the framework reads, uses no
// {placeholder} the English text does not fill, keeps every printf verb
// in order, and covers every framework string that is words.
func TestSpanishCatalogMatchesDefaults(t *testing.T) {
	es := flatCatalog(t, "es.json")
	for k, v := range es {
		if strings.HasPrefix(k, "entity.") {
			continue
		}
		en, ok := i18nui.Defaults[i18nui.Key(k)]
		if !ok {
			t.Errorf("%s is not a framework key", k)
			continue
		}
		for _, p := range bracePlaceholder.FindAllString(v, -1) {
			if !strings.Contains(en, p) {
				t.Errorf("%s: %s is not filled for this key", k, p)
			}
		}
		if !slices.Equal(printfVerb.FindAllString(v, -1), printfVerb.FindAllString(en, -1)) {
			t.Errorf("%s: printf verbs differ from %q", k, en)
		}
	}
	for k, en := range i18nui.Defaults {
		if _, ok := es[string(k)]; !ok && strings.ContainsAny(en, "abcdefghijklmnopqrstuvwxyz") && !strings.HasPrefix(string(k), "ui.json.") {
			t.Errorf("%s (%q) has no Spanish", k, en)
		}
	}
}

// The catalog's entity keys name entities, fields, values, views and
// transitions gofastr.yml declares, so a rename cannot strand a
// translation.
func TestSpanishCatalogNamesDeclaredEntities(t *testing.T) {
	raw, err := os.ReadFile("gofastr.yml")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Entities []struct {
			Name   string `yaml:"name"`
			Fields []struct {
				Name   string   `yaml:"name"`
				Values []string `yaml:"values"`
			} `yaml:"fields"`
			Display struct {
				Views []struct {
					Key string `yaml:"key"`
				} `yaml:"views"`
			} `yaml:"display"`
			States struct {
				Transitions []struct {
					Key string `yaml:"key"`
				} `yaml:"transitions"`
			} `yaml:"states"`
		} `yaml:"entities"`
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, e := range spec.Entities {
		base := "entity." + e.Name + "."
		for _, k := range []string{"singular", "plural", "description",
			// The timestamps every entity carries.
			"fields.created_at.label", "fields.updated_at.label"} {
			known[base+k] = true
		}
		for _, f := range e.Fields {
			known[base+"fields."+f.Name+".label"] = true
			known[base+"fields."+f.Name+".help"] = true
			for _, v := range f.Values {
				known[base+"fields."+f.Name+".values."+v] = true
			}
		}
		for _, v := range e.Display.Views {
			known[base+"views."+v.Key] = true
		}
		for _, tr := range e.States.Transitions {
			known[base+"transitions."+tr.Key] = true
		}
	}
	if len(known) == 0 {
		t.Fatal("setup: gofastr.yml declared nothing")
	}
	for k := range flatCatalog(t, "es.json") {
		if strings.HasPrefix(k, "entity.") && !known[k] {
			t.Errorf("%s names nothing gofastr.yml declares", k)
		}
	}
}
