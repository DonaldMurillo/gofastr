package dsl

import (
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

func hiddenDSLRegistry() dslRegistry {
	ent := entity.Define("users", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String},
			{Name: "pw_hash", Type: schema.String, Hidden: true},
		},
	})
	return dslRegistry{ents: map[string]*entity.Entity{"users": ent}}
}

func TestBuildDSLHiddenReadsAsAbsent(t *testing.T) {
	reg := hiddenDSLRegistry()
	for _, pair := range [][2]string{
		{`users.where(pw_hash="x")`, `users.where(nope="x")`},
		{`users.order(pw_hash ASC)`, `users.order(nope ASC)`},
	} {
		_, hidden := BuildDSLQuery(reg, pair[0])
		_, absent := BuildDSLQuery(reg, pair[1])
		if hidden == nil || absent == nil {
			t.Fatalf("%s: both must be refused, got %v and %v", pair[0], hidden, absent)
		}
		if strings.ReplaceAll(hidden.Error(), "pw_hash", "F") != strings.ReplaceAll(absent.Error(), "nope", "F") {
			t.Errorf("Hidden and absent refusals differ: %q vs %q", hidden, absent)
		}
	}
}

func TestBuildDSLSelectsNoHiddenColumn(t *testing.T) {
	qb, err := BuildDSLQuery(hiddenDSLRegistry(), `users.limit(5)`)
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := qb.Build()
	if !strings.HasPrefix(sql, "SELECT id, name, created_at, updated_at FROM ") || strings.Contains(sql, "pw_hash") {
		t.Fatalf("the projection must leave Hidden columns out: %s", sql)
	}
}

func TestParseDSLCacheHitIsolated(t *testing.T) {
	const q = `posts.where(status="hit").limit(3)`
	if _, err := ParseDSL(q); err != nil {
		t.Fatal(err)
	}
	second, _ := ParseDSL(q)
	second.Filters[0].Value = "PWNED"
	if third, _ := ParseDSL(q); third.Filters[0].Value != "hit" {
		t.Fatalf("a cache hit must return a copy, got %q", third.Filters[0].Value)
	}
}

func TestKeywordIsAWholeWord(t *testing.T) {
	fields := []schema.Field{{Name: "a", Type: schema.Int}, {Name: "y", Type: schema.Int}}
	if _, err := ParsePredicate("a = 1 andy = 2", fields); err == nil {
		t.Fatal("andy must not read as the keyword and")
	}
}

func TestBracketClosedBeforeOpenedRefused(t *testing.T) {
	if _, err := ParseDSL(`posts.where(title=a], seq=b[)`); err == nil {
		t.Fatal("a bracket closed before it opens must be refused even when the count balances")
	}
}

func TestParseSortRefusesNoQueryByName(t *testing.T) {
	_, err := ParseSort("card", []schema.Field{{Name: "card", Type: schema.String, NoQuery: true}})
	if err == nil || !strings.Contains(err.Error(), "cannot be sorted") {
		t.Fatalf("a NoQuery sort must be refused by name, got %v", err)
	}
}

func TestParseSortAgreesWithFilterSort(t *testing.T) {
	fields := []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "author_id", Type: schema.String, WireName: "writer"},
		{Name: "secret", Type: schema.String, Hidden: true},
		{Name: "card", Type: schema.String, NoQuery: true},
	}
	for _, name := range []string{"title", "writer", "author_id", "secret", "card", "nope"} {
		got, dslErr := ParseSort(name+" DESC", fields)
		want, filterErr := filter.ParseSortValues(url.Values{"sort": {"-" + name}}, fields)
		if (dslErr == nil) != (filterErr == nil) {
			t.Errorf("%s: dsl err %v, filter err %v", name, dslErr, filterErr)
			continue
		}
		if dslErr == nil && (got[0].Field != want[0].Field || got[0].Desc != want[0].Desc) {
			t.Errorf("%s: dsl %+v, filter %+v", name, got[0], want[0])
		}
	}
}

func TestParseSortRefusesWireNameOutsideIdentSyntax(t *testing.T) {
	fields := []schema.Field{{Name: "title", WireName: "display-name", Type: schema.String}}
	got, err := filter.ParseSortValues(url.Values{"sort": {"-display-name"}}, fields)
	if err != nil || len(got) != 1 || got[0].Field != "title" {
		t.Fatalf("filter.ParseSortValues should resolve the wire name to title: %+v, %v", got, err)
	}
	if _, err := ParseSort("display-name DESC", fields); err == nil {
		t.Fatal("ParseSort accepted a name outside the DSL identifier syntax")
	}
}
