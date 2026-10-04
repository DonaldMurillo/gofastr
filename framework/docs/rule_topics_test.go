package docs

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	_ "github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
)

// TestEveryRuleDocTopicExists pins the link every diagnostic advertises.
// A rule's Doc field becomes "gofastr docs <topic>" in the report and a
// URL in the SARIF output; pointing either at a topic that does not exist
// turns the most useful line in a finding into a dead end.
//
// This lives beside the docs rather than in contracts because the
// catalog must not import the docs package: the rules have to be
// readable and serveable without dragging the embedded markdown in.
// (It lived in package framework until framework stopped importing
// docs, #470.)
func TestEveryRuleDocTopicExists(t *testing.T) {
	topics, err := List()
	if err != nil {
		t.Fatal(err)
	}
	known := make(map[string]bool, len(topics))
	for _, top := range topics {
		known[top.Name] = true
	}
	for _, r := range contracts.AllRules() {
		if r.Doc == "" {
			t.Errorf("%s has no doc topic", r.ID)
			continue
		}
		if !known[r.Doc] {
			t.Errorf("%s points at doc topic %q, which does not exist", r.ID, r.Doc)
		}
	}
}
