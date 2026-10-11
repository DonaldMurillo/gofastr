package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// App tokens at verify time: a <name>.tokens.css is checked with the
// call gen styles makes, its tokens are known to every owned sheet in
// the program, and its generated sibling is held fresh by 1814.

// tokensPair returns a tokens file and its fresh generated sibling.
func tokensPair(t *testing.T, dir, pkg, name, css string) map[string]string {
	t.Helper()
	f, diags := ownstyle.ParseTokens(css)
	for _, d := range diags {
		if d.Severity == ownstyle.SeverityError {
			t.Fatalf("fixture tokens file does not parse: %v", diags)
		}
	}
	gen, err := ownstyle.GenerateTokensFile(name, css, f, pkg, false)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		dir + "/" + ownstyle.TokensFileName(name):          css,
		dir + "/" + ownstyle.GeneratedTokensFileName(name): gen,
	}
}

const heroGap = `@property --size-hero-gap { syntax: "<length>"; inherits: true; initial-value: clamp(2rem, 6vw, 5rem); }
`

func TestAppTokensAreKnownToOwnedSheets(t *testing.T) {
	sheet := ownedPair(t, "home", "home", "hero", ":scope { padding-block: var(--size-hero-gap); }\n")
	assertHas(t, fixture(t, sheet), contracts.RuleUnknownThemeToken)
	ds := fixture(t, merge(sheet, tokensPair(t, "theme", "theme", "acme", heroGap)))
	assertNot(t, ds, contracts.RuleUnknownThemeToken, "the token is declared in theme/acme.tokens.css")
	assertNot(t, ds, contracts.RuleOwnerlessStylesheet, "a tokens file has its owner: the package beside it")
	assertNot(t, ds, contracts.RuleStaleStyleSource, "the tokens file's Go is fresh")
}

func TestAppTokenLiteralIs1807(t *testing.T) {
	ds := fixture(t, merge(
		tokensPair(t, "theme", "theme", "acme", heroGap),
		ownedPair(t, "home", "home", "hero", ":scope { min-height: clamp(2rem, 6vw, 5rem); }\n"),
	))
	found := countRule(t, ds, contracts.RuleHardcodedTokenValue)
	if len(found) != 1 || !strings.Contains(found[0].Message, "--size-hero-gap") {
		t.Errorf("want the app token named, got %v", found)
	}
}

func TestStaleTokensFileIs1814(t *testing.T) {
	pair := tokensPair(t, "theme", "theme", "acme", heroGap)
	pair["theme/acme.tokens.css"] = strings.Replace(heroGap, "5rem", "6rem", 1)
	found := countRule(t, fixture(t, pair), contracts.RuleStaleStyleSource)
	if len(found) != 1 || found[0].File != "theme/acme.tokens.css" {
		t.Errorf("want 1814 on the edited tokens file, got %v", found)
	}
	missing := map[string]string{"theme/acme.tokens.css": heroGap, "theme/theme.go": "package theme\n"}
	found = countRule(t, fixture(t, missing), contracts.RuleStaleStyleSource)
	if len(found) != 1 || !strings.Contains(found[0].Message, "acme_tokens.gen.go") {
		t.Errorf("want 1814 naming the missing sibling, got %v", found)
	}
}

func TestDuplicateTokenValueAtVerify(t *testing.T) {
	css := "@property --color-brand { syntax: \"<color>\"; inherits: true; initial-value: #18181B; }\n"
	ds := fixture(t, tokensPair(t, "theme", "theme", "acme", css))
	found := countRule(t, ds, contracts.RuleDuplicateTokenValue)
	if len(found) != 1 || found[0].File != "theme/acme.tokens.css" || found[0].Line != 1 || found[0].Column != 11 {
		t.Fatalf("want one 1821 at theme/acme.tokens.css:1:11, got %v", found)
	}
	waived := "/* gofastr:allow(GOFASTR1821) the brand blue matches primary on purpose until the rebrand */\n" + css
	assertNot(t, fixture(t, tokensPair(t, "theme", "theme", "acme", waived)), contracts.RuleDuplicateTokenValue, "an allow marker with a reason")
}

func TestRepeatedLiteralAtVerify(t *testing.T) {
	ds := fixture(t, merge(
		ownedPair(t, "article", "article", "article", ".body { max-width: 37rem; }\n"),
		ownedPair(t, "help", "help", "help", ".answer { max-width: 37rem; }\n"),
	))
	found := countRule(t, ds, contracts.RuleRepeatedLiteral)
	if len(found) != 2 {
		t.Fatalf("want both sites of the repeated literal, got %v", found)
	}
	for _, d := range found {
		if d.Severity != contracts.SeverityWarn {
			t.Errorf("1822 is a warning, got %s", d.Severity)
		}
	}
	ds = fixture(t, merge(
		tokensPair(t, "theme", "theme", "acme", "@property --size-reading-width { syntax: \"<length>\"; inherits: true; initial-value: 37rem; }\n"),
		ownedPair(t, "article", "article", "article", ".body { max-width: var(--size-reading-width); }\n"),
		ownedPair(t, "help", "help", "help", ".answer { max-width: var(--size-reading-width); }\n"),
	))
	assertNot(t, ds, contracts.RuleRepeatedLiteral, "the documented fix: one token, read in both sheets")
}

func TestOwnedFallbackNoLongerWaives1806(t *testing.T) {
	fireAndQuiet(t, contracts.RuleUnknownThemeToken, "board",
		".a { color: var(--host-ink, #111); }\n", 1, 17,
		"/* gofastr:allow(GOFASTR1806) the embedding page sets --host-ink */\n.a { color: var(--host-ink, #111); }\n")
}
