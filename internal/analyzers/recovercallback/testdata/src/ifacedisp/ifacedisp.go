// Package ifacedisp pins the interface-dispatch leg (review finding
// R1): a method call through an interface declared in this package
// reaches every package-local method with the same name and arity —
// the ordinary Go way to decouple a transport from its handlers, and
// precisely how a dispatch path is structured once a peer grows past
// one file's coupling.
package ifacedisp

type Frame struct{ Method string }

type Reader interface{ ReadFrame() (Frame, error) }

type Disp interface{ Dispatch(Frame) }

type Peer struct {
	r Reader
	d Disp
}

func (p *Peer) Serve() error {
	for {
		f, err := p.r.ReadFrame()
		if err != nil {
			return err
		}
		// ReadFrame stays quiet (input plumbing, the Read name family);
		// Dispatch through the package interface FIELD is the extension
		// point itself — the cron runTick shape.
		p.d.Dispatch(f) // want `recovercallback: p\.d\.Dispatch is invoked with no recover in scope`
	}
}

type Impl struct{ gate func(Frame) error }

func (i *Impl) Dispatch(f Frame) {
	_ = i.gate(f) // want `recovercallback: i\.gate is invoked with no recover in scope`
}

// Unrelated Dispatch-arity methods are not edges: same name, different
// arity.
type Other struct{}

func (o *Other) Dispatch(f Frame, extra int) {
	_ = f
}
