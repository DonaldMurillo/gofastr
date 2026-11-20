package toolargs

import (
	"context"
	"errors"
	"fmt"

	"github.com/DonaldMurillo/gofastr/kiln/protocol"
)

// ErrUnknownTool wraps the error Call returns for a name with no tool.
var ErrUnknownTool = errors.New("unknown tool")

type toolFunc func(t *protocol.Tools, ctx context.Context, body []byte) (protocol.Result, error)

func typed[A any](method func(*protocol.Tools, context.Context, A) protocol.Result) toolFunc {
	return func(t *protocol.Tools, ctx context.Context, body []byte) (protocol.Result, error) {
		var a A
		if err := Decode(body, &a); err != nil {
			return protocol.Result{}, err
		}
		return method(t, ctx, a), nil
	}
}

// optional is typed, except an empty body means the zero args.
func optional[A any](method func(*protocol.Tools, context.Context, A) protocol.Result) toolFunc {
	decode := typed(method)
	return func(t *protocol.Tools, ctx context.Context, body []byte) (protocol.Result, error) {
		if len(body) == 0 {
			var a A
			return method(t, ctx, a), nil
		}
		return decode(t, ctx, body)
	}
}

// ignored calls method with the zero args and never reads the body.
func ignored[A any](method func(*protocol.Tools, context.Context, A) protocol.Result) toolFunc {
	return func(t *protocol.Tools, ctx context.Context, _ []byte) (protocol.Result, error) {
		var a A
		return method(t, ctx, a), nil
	}
}

var tools = map[string]toolFunc{
	"world_get":           optional((*protocol.Tools).WorldGet),
	"set_app_config":      typed((*protocol.Tools).SetAppConfig),
	"set_scaffold":        typed((*protocol.Tools).SetScaffold),
	"add_entity":          typed((*protocol.Tools).AddEntity),
	"update_entity":       typed((*protocol.Tools).UpdateEntity),
	"delete_entity":       typed((*protocol.Tools).DeleteEntity),
	"add_field":           typed((*protocol.Tools).AddField),
	"delete_field":        typed((*protocol.Tools).DeleteField),
	"add_page":            typed((*protocol.Tools).AddPage),
	"delete_page":         typed((*protocol.Tools).DeletePage),
	"update_page_element": typed((*protocol.Tools).UpdatePageElement),
	"add_hook":            typed((*protocol.Tools).AddHook),
	"delete_hook":         typed((*protocol.Tools).DeleteHook),
	"add_route":           typed((*protocol.Tools).AddRoute),
	"delete_route":        typed((*protocol.Tools).DeleteRoute),
	"add_seed":            typed((*protocol.Tools).AddSeed),
	"propose_plan":        typed((*protocol.Tools).ProposePlan),
	"approve_plan":        typed((*protocol.Tools).ApprovePlan),
	"reject_plan":         typed((*protocol.Tools).RejectPlan),
	"undo":                ignored((*protocol.Tools).Undo),
	"reset_session":       ignored((*protocol.Tools).ResetSession),
	"set_theme":           typed((*protocol.Tools).SetTheme),
	"chat":                typed((*protocol.Tools).Chat),
}

// Call decodes body into the named tool's args with Decode and runs the
// tool. A decode failure is returned as the error; a name with no tool
// returns an error wrapping ErrUnknownTool.
func Call(ctx context.Context, t *protocol.Tools, name string, body []byte) (protocol.Result, error) {
	fn, ok := tools[name]
	if !ok {
		return protocol.Result{}, fmt.Errorf("%w %q", ErrUnknownTool, name)
	}
	return fn(t, ctx, body)
}
