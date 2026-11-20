package i18n

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
)

// Phrase is one user-facing string: its catalog key and English text. Make
// one with [Define] at package level.
type Phrase struct {
	key, english string
}

var (
	phrasesMu sync.RWMutex
	phrases   = map[string]string{}
)

// Define registers a phrase; it panics on a malformed key, empty English, or
// a key already defined with other English.
func Define(key, english string) Phrase {
	if err := checkPhraseKey(key); err != nil {
		panic("i18n.Define: " + err.Error())
	}
	if english == "" {
		panic(fmt.Sprintf("i18n.Define(%q): empty English text", key))
	}
	phrasesMu.Lock()
	defer phrasesMu.Unlock()
	if prev, ok := phrases[key]; ok && prev != english {
		panic(fmt.Sprintf("i18n.Define(%q): already defined as %q", key, prev))
	}
	phrases[key] = english
	return Phrase{key: key, english: english}
}

func checkPhraseKey(key string) error {
	segs := strings.Split(key, ".")
	if len(segs) < 2 {
		return fmt.Errorf("key %q needs a namespace (a.b)", key)
	}
	for _, s := range segs {
		if s == "" {
			return fmt.Errorf("key %q has an empty segment", key)
		}
		for i := 0; i < len(s); i++ {
			c := s[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
				return fmt.Errorf("key %q has byte %#x outside [A-Za-z0-9_-]", key, c)
			}
		}
	}
	return nil
}

// Phrases returns every defined key with its English text.
func Phrases() map[string]string {
	phrasesMu.RLock()
	defer phrasesMu.RUnlock()
	return maps.Clone(phrases)
}

// Key is the catalog key.
func (p Phrase) Key() string { return p.key }

// English is the text shown when no catalog has the key.
func (p Phrase) English() string { return p.english }

// Code is the key's last segment, the stable code clients branch on.
func (p Phrase) Code() string { return p.key[strings.LastIndexByte(p.key, '.')+1:] }

// Format is p's English text with params filled.
func (p Phrase) Format(params ...map[string]any) string {
	return Fill(p.english, mergeParams(params))
}

// IsZero reports whether p was never defined.
func (p Phrase) IsZero() bool { return p.key == "" }

// Text translates p for ctx's locale through ctx's translator, else
// [Default], filling params; the zero Phrase renders "".
func (p Phrase) Text(ctx context.Context, params ...map[string]any) string {
	if p.key == "" {
		return ""
	}
	merged := translateParams(ctx, mergeParams(params))
	tr := TranslatorFrom(ctx)
	if tr == nil {
		tr = Default()
	}
	if tr != nil {
		if s := tr.T(ctx, p.key, merged); s != p.key {
			return s
		}
	}
	return fill(p.english, merged, cldrEnglish().loc)
}

func innermostCode(outer Phrased, code string) string {
	var cur error = outer
	for next := errors.Unwrap(cur); next != nil; next = errors.Unwrap(cur) {
		in, ok := errors.AsType[Phrased](next)
		if !ok {
			break
		}
		if p, _ := in.Phrased(); !p.IsZero() {
			code = p.Code()
		}
		cur = in
	}
	return code
}

func translateParams(ctx context.Context, params map[string]any) map[string]any {
	for k, v := range params {
		if e, ok := v.(Phrased); ok {
			if ph, pp := e.Phrased(); !ph.IsZero() {
				params[k] = ph.Text(ctx, pp)
			}
		}
	}
	return params
}

// Phrased is an error whose message is a phrase.
type Phrased interface {
	error
	Phrased() (Phrase, map[string]any)
}

// Error is the plain [Phrased] error; Error() is the English message.
type Error struct {
	Reason Phrase
	Params map[string]any
	Err    error // wrapped cause (optional), never shown
}

// NewError returns an *Error for p with params.
func NewError(p Phrase, params ...map[string]any) *Error {
	return &Error{Reason: p, Params: mergeParams(params)}
}

// Wrap returns an *Error for p whose {{detail}} is err, which it wraps.
func Wrap(p Phrase, err error, params ...map[string]any) *Error {
	m := mergeParams(params)
	m["detail"] = err
	return &Error{Reason: p, Params: m, Err: err}
}

func (e *Error) Error() string { return e.Reason.Format(e.Params) }

func (e *Error) Unwrap() error { return e.Err }

// Phrased implements [Phrased].
func (e *Error) Phrased() (Phrase, map[string]any) { return e.Reason, e.Params }

// ErrorText renders err for ctx's locale with the innermost phrased code; an
// unphrased err fills frame's {{detail}}, or with a zero frame is its own text.
func ErrorText(ctx context.Context, err error, frame Phrase) (text, code string) {
	if e, ok := errors.AsType[Phrased](err); ok {
		if p, params := e.Phrased(); !p.IsZero() {
			return p.Text(ctx, params), innermostCode(e, p.Code())
		}
	}
	if frame.IsZero() {
		return err.Error(), ""
	}
	return frame.Text(ctx, map[string]any{"detail": err.Error()}), frame.Code()
}

type translatorCtxKey struct{}

// WithTranslator attaches tr to ctx.
func WithTranslator(ctx context.Context, tr *Translator) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, translatorCtxKey{}, tr)
}

// TranslatorFrom returns the translator on ctx, or nil.
func TranslatorFrom(ctx context.Context) *Translator {
	if ctx == nil {
		return nil
	}
	tr, _ := ctx.Value(translatorCtxKey{}).(*Translator)
	return tr
}
