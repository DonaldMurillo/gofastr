// Package component defines the component model for GoFastr's core-ui framework.
//
// A Component describes its visual appearance via the Render method, returning
// render.HTML. Components that handle user events implement the
// InteractiveComponent interface by adding an Actions method.
//
// The package provides:
//   - Component and InteractiveComponent interfaces
//   - ComponentBase for embedding common fields (ID, Class)
//   - ComponentContext for event data and state access
//   - ActionRegistry and On() for declarative event handling
//   - Lifecycle hooks (Mount, Update, Unmount)
//   - ComponentList for rendering multiple components
//
// Basic usage:
//
//	type Greeting struct {
//	    component.ComponentBase
//	    Name string
//	}
//
//	func (g *Greeting) Render() render.HTML {
//	    return render.Tag("p", nil, render.Text("Hello, "+g.Name))
//	}
package component
