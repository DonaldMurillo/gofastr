package i18n

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
)

// Phrase is one user-facing string: the catalog key it is looked up
// under and the English text used when no catalog has it. Make one with
// [Define] at package level; every Phrase the program can show is then
// in [Phrases], which is what catalog completeness tests read.
//
// The last segment of the key is the phrase's stable code: an error
// body names "not_found" for the phrase under "error.not_found", so a
// client can branch on it whatever language the message is in.
type Phrase struct {
	key, english string
}

var (
	phrasesMu sync.RWMutex
	phrases   = map[string]string{}
)

// Define registers a phrase and returns it. It panics on a malformed
// key or empty English, and on a key already defined with different
// English: two packages cannot claim one key for two sentences.
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

// checkPhraseKey accepts dot-separated segments of ASCII letters,
// digits, '_' and '-', with at least two segments.
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

// Format is p's English text with params interpolated: the locale-free
// rendering an error's Error() method and a log line use.
func (p Phrase) Format(params ...map[string]any) string {
	return interpolate(p.english, mergeParams(params))
}

// IsZero reports whether p was never defined.
func (p Phrase) IsZero() bool { return p.key == "" }

// Text translates p for ctx's locale through the translator on ctx (see
// [WithTranslator]), else the [Default] one, interpolating {{name}}
// placeholders from params. A param that is a [Phrased] error renders
// as its own Text, so a wrapped cause translates with its frame. A
// catalog miss renders the English text with the same params. The zero
// Phrase renders "".
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
	return interpolate(p.english, merged)
}

// innermostCode walks the [Phrased] errors under outer and returns the
// last non-zero code, or code when none is deeper.
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

// translateParams replaces each [Phrased] value in params with its text
// for ctx. Format needs no twin: interpolate prints a value with
// fmt.Fprint, which is the error's English Error().
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

// Phrased is an error that names the phrase its message is. An error
// writer translates any error in whose chain one sits ([ErrorText]).
type Phrased interface {
	error
	Phrased() (Phrase, map[string]any)
}

// Error is the plain [Phrased] error. A package whose errors reach a
// client (a filter parser, a cursor decoder) returns one, and the error
// writer translates it; Error() is the English message.
type Error struct {
	Reason Phrase
	Params map[string]any
	Err    error // wrapped cause (optional), never shown
}

// NewError returns an *Error for p with params.
func NewError(p Phrase, params ...map[string]any) *Error {
	return &Error{Reason: p, Params: mergeParams(params)}
}

// Wrap returns an *Error for p that wraps err. err fills p's {{detail}}
// (translated when it is [Phrased]) and stays reachable through
// errors.Is and errors.As. Use it where fmt.Errorf("context: %w", err)
// would put English in front of a phrased cause.
func Wrap(p Phrase, err error, params ...map[string]any) *Error {
	m := mergeParams(params)
	m["detail"] = err
	return &Error{Reason: p, Params: m, Err: err}
}

func (e *Error) Error() string { return e.Reason.Format(e.Params) }

func (e *Error) Unwrap() error { return e.Err }

// Phrased implements [Phrased].
func (e *Error) Phrased() (Phrase, map[string]any) { return e.Reason, e.Params }

// ErrorText renders err for ctx's locale and names its code. The text
// is the first [Phrased] error in err's chain, which renders the causes
// it wraps (see [Wrap]); the code is the innermost one's, the most
// specific: a nested filter refusal that wraps an unknown filter
// answers unknown_filter. With no phrased error, a non-zero frame
// renders with err's text as its {{detail}}; with a zero frame the text
// is err's own and the code is empty: an app's own wording, sent as
// written.
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

// WithTranslator attaches tr to ctx, so [Phrase.Text] and the
// framework's UI strings resolve through it. [Middleware] does this for
// every request.
func WithTranslator(ctx context.Context, tr *Translator) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, translatorCtxKey{}, tr)
}

// TranslatorFrom returns the translator attached by [WithTranslator],
// or nil.
func TranslatorFrom(ctx context.Context) *Translator {
	if ctx == nil {
		return nil
	}
	tr, _ := ctx.Value(translatorCtxKey{}).(*Translator)
	return tr
}
