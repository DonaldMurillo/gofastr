// Package lib reduces core-ui/app: screen hook interfaces and the
// recover guards around them.
package lib

import "context"

type Titler interface {
	Title(ctx context.Context) string
}

type Langer interface {
	Lang() string
}

// Meta embeds Titler: its Title is the same method object.
type Meta interface {
	Titler
	Describe() string
}

type Describer interface {
	Describe() string
}

type Closer interface {
	Close() error
}

// SafeTitle is SafeScreenTitle.
func SafeTitle(ctx context.Context, t Titler) (title string) { // want SafeTitle:"recover guard for Title"
	defer func() {
		if r := recover(); r != nil {
			title = ""
		}
	}()
	return t.Title(ctx)
}

// SafeLang recovers through a named deferred helper.
func SafeLang(l Langer) (lang string) { // want SafeLang:"recover guard for Lang"
	defer swallow()
	return l.Lang()
}

func swallow() {
	_ = recover()
}

// safeDescribe is unexported: it guards Describe in lib only.
func safeDescribe(d Describer) (s string) {
	defer func() {
		if recover() != nil {
			s = ""
		}
	}()
	return d.Describe()
}

type Writer interface {
	Write(p []byte) (int, error)
}

// Busy recovers around more than one call, a handler's shape: it is
// not a guard for Write.
func Busy(w Writer) (err error) {
	defer func() { _ = recover() }()
	_, _ = w.Write([]byte("a"))
	_, err = w.Write([]byte("b"))
	return err
}

func write(w Writer) {
	_, _ = w.Write(nil)
}

// rethrow recovers and panics again: it guards nothing.
func rethrow(c Closer) error {
	defer func() {
		if r := recover(); r != nil {
			panic(r)
		}
	}()
	return c.Close()
}

// head is the bypass: a second path calling the hook bare.
func head(t Titler) string {
	return t.Title(context.Background()) // want `Title is called directly, but SafeTitle exists to recover its panic: call SafeTitle \(or recover here\)`
}

func describe(d Describer) string {
	return d.Describe() // want `Describe is called directly, but safeDescribe exists`
}

func closeIt(c Closer) error {
	return c.Close()
}

var _ = []any{head, describe, closeIt, safeDescribe, rethrow, swallow, write}
