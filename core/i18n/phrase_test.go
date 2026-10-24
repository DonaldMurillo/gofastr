package i18n

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

var testPhrase = Define("test.phrase.too_long", "must be at most {{max}} characters")

func TestPhraseEnglishWithoutTranslator(t *testing.T) {
	got := testPhrase.Text(context.Background(), map[string]any{"max": 80})
	if got != "must be at most 80 characters" {
		t.Fatalf("Text = %q", got)
	}
}

func TestPhraseUsesContextTranslator(t *testing.T) {
	cat := NewMapCatalog()
	cat.Set("fr", "test.phrase.too_long", Message{Text: "{{max}} caractères au plus"})
	ctx := WithTranslator(WithContext(context.Background(), Locale{Tag: "fr"}), NewTranslator(cat, "en"))
	if got := testPhrase.Text(ctx, map[string]any{"max": 80}); got != "80 caractères au plus" {
		t.Fatalf("Text = %q", got)
	}
}

func TestPhraseCatalogMissFallsBack(t *testing.T) {
	ctx := WithTranslator(WithContext(context.Background(), Locale{Tag: "fr"}), NewTranslator(NewMapCatalog(), "en"))
	if got := testPhrase.Text(ctx, map[string]any{"max": 3}); got != "must be at most 3 characters" {
		t.Fatalf("Text = %q", got)
	}
}

func TestPhraseCodeIsLastSegment(t *testing.T) {
	if got := testPhrase.Code(); got != "too_long" {
		t.Fatalf("Code = %q", got)
	}
	if testPhrase.Key() != "test.phrase.too_long" || testPhrase.English() != "must be at most {{max}} characters" {
		t.Fatalf("Key/English = %q / %q", testPhrase.Key(), testPhrase.English())
	}
}

func TestPhrasesListsDefinitions(t *testing.T) {
	if got := Phrases()["test.phrase.too_long"]; got != testPhrase.English() {
		t.Fatalf("Phrases()[key] = %q", got)
	}
}

func TestDefineRefusesConflict(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("redefining a key with other English did not panic")
		}
	}()
	Define("test.phrase.too_long", "something else")
}

func TestDefineRefusesBadKeys(t *testing.T) {
	for _, key := range []string{"", "nodot", "has space.x", "trailing.", ".leading", "ctl.\x01"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Define(%q) did not panic", key)
				}
			}()
			Define(key, "text")
		}()
	}
}

var testFrame = Define("test.phrase.bad_filter", "invalid filters: {{detail}}")

func TestErrorTextPrefersPhrasedError(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", NewError(testPhrase, map[string]any{"max": 2}))
	text, code := ErrorText(context.Background(), err, testFrame)
	if text != "must be at most 2 characters" || code != "too_long" {
		t.Fatalf("got %q %q", text, code)
	}
}

var testWrapFrame = Define("test.phrase.nested", "nested {{key}}: {{detail}}")

// A wrapped phrased cause translates inside its frame, the code is the
// cause's, and Error() stays English.
func TestWrapTranslatesCause(t *testing.T) {
	cat := NewMapCatalog()
	cat.Set("fr", "test.phrase.too_long", Message{Text: "{{max}} caractères au plus"})
	cat.Set("fr", "test.phrase.nested", Message{Text: "imbriqué {{key}} : {{detail}}"})
	ctx := WithTranslator(WithContext(context.Background(), Locale{Tag: "fr"}), NewTranslator(cat, "en"))
	cause := NewError(testPhrase, map[string]any{"max": 2})
	err := Wrap(testWrapFrame, cause, map[string]any{"key": "a.b"})
	text, code := ErrorText(ctx, err, testFrame)
	if text != "imbriqué a.b : 2 caractères au plus" || code != "too_long" {
		t.Fatalf("got %q %q", text, code)
	}
	if got := err.Error(); got != "nested a.b: must be at most 2 characters" {
		t.Fatalf("Error() = %q", got)
	}
	if !errors.Is(err, cause) {
		t.Fatal("Wrap hides its cause from errors.Is")
	}
}

// A frame around a plain error keeps its own code.
func TestWrapPlainCauseKeepsFrameCode(t *testing.T) {
	err := Wrap(testWrapFrame, errors.New("boom"), map[string]any{"key": "k"})
	text, code := ErrorText(context.Background(), err, testFrame)
	if text != "nested k: boom" || code != "nested" {
		t.Fatalf("got %q %q", text, code)
	}
}

func TestErrorTextFramesPlainError(t *testing.T) {
	text, code := ErrorText(context.Background(), errors.New("unknown op"), testFrame)
	if text != "invalid filters: unknown op" || code != "bad_filter" {
		t.Fatalf("got %q %q", text, code)
	}
}

func TestErrorTextZeroFrameKeepsAppText(t *testing.T) {
	text, code := ErrorText(context.Background(), errors.New("stock too low"), Phrase{})
	if text != "stock too low" || code != "" {
		t.Fatalf("got %q %q", text, code)
	}
}

func TestZeroPhraseTextIsEmpty(t *testing.T) {
	var p Phrase
	if got := p.Text(context.Background()); got != "" {
		t.Fatalf("zero Phrase Text = %q", got)
	}
}
