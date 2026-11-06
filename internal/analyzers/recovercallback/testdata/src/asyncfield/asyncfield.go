// Package asyncfield pins the goroutine-launch and method-value legs:
// a callback expression launched with `go` has no recover by
// construction (the enclosing function's deferred recover runs on a
// different stack, and the function need not sit on any dispatch path),
// and a method VALUE taken from a host-installed extension-point slot
// is host code exactly like the direct call through it.
package asyncfield

type Bus interface{ Push(event string) }

type Server struct {
	bus Bus
}

func (s *Server) Start() {
	go s.bus.Push("ready") // want `recovercallback: s\.bus\.Push is invoked with no recover in scope`
}

// A func-typed field launched the same way.
type Runner struct {
	onTick func()
}

func (r *Runner) Start() {
	go r.onTick() // want `recovercallback: r\.onTick is invoked with no recover in scope`
}

// The literal spelling was already covered: the goroutine literal is a
// seed and its own call reports.
func (s *Server) StartViaLiteral() {
	go func() {
		s.bus.Push("later") // want `recovercallback: s\.bus\.Push is invoked with no recover in scope`
	}()
}

// A plain package function launched with go is a seed like any other:
// quiet unless its own dispatch path reaches a callback.
func (s *Server) StartPlain() {
	go plain()
}

func plain() {}

// The method-value alias: pub holds the host's Publish, one hop from
// the field, and the range loop is the dispatch path.
type Backend interface{ Publish(ev string) }

type Feed struct {
	backend Backend
	events  chan string
}

func (f *Feed) Run() error {
	for ev := range f.events {
		pub := f.backend.Publish
		pub(ev) // want `recovercallback: pub is invoked with no recover in scope`
	}
	return nil
}

// A method value off a concrete local type is package code the edge
// flood already reaches: quiet.
type local struct{}

func (l local) Ping(ev string) {}

func quietAlias(ev string) {
	l := local{}
	p := l.Ping
	p(ev)
}
