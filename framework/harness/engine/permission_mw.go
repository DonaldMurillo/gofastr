package engine

import (
	"context"
	"errors"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool/permission"
)

// PermissionMiddleware returns a tool.Middleware that consults the
// permission engine before invoking the wrapped tool. The middleware:
//
//   - Computes the argv summary for the call (FormatArgvSummary).
//   - Asks the permission engine for a Decision.
//   - On DecisionAllow, calls next.
//   - On DecisionDeny, returns an error result.
//   - On DecisionAsk, publishes a PermissionRequested event and
//     blocks waiting for an AnswerPermission via the answer
//     channel (typically wired by the multiplexer). Times out per
//     PermissionTimeout; on timeout, denies the call.
//
// Per hard rule 11, the multiplexer enforces "agents cannot
// self-approve their own turn" before delivering an answer to this
// middleware. This middleware does not re-check identity.
//
// PermissionTimeout is the maximum time to wait for a human ack
// before denying. The doc commits to 60s as default.
type PermissionAnswer struct {
	CallID ids.CallID
	Allow  bool
	Scope  control.PermitScope
	Source ids.ClientID
}

// AnswerRouter is wired by the multiplexer. It returns a channel that
// receives AnswerPermission events for the given session, and a
// function that subscribes the caller for the lifetime of a single
// PermissionRequested. The middleware uses Subscribe to wait for a
// matching answer; the multiplexer narrows by CallID and identity
// rules before forwarding.
type AnswerRouter interface {
	Subscribe(session ids.SessionID, callID ids.CallID) <-chan PermissionAnswer
	Unsubscribe(session ids.SessionID, callID ids.CallID)
}

// PermissionMiddleware constructs the middleware.
//
// session is bound at middleware-construction time (one engine = one
// session); originator is also fixed per-turn and supplied via
// context (so the multiplexer can address the prompt to the right
// client).
func PermissionMiddleware(
	bus *Bus,
	eng *permission.Engine,
	router AnswerRouter,
	session ids.SessionID,
	timeout time.Duration,
) tool.Middleware {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return func(ctx context.Context, call tool.ToolCall, sink tool.EventSink, next tool.Handler) (*tool.ToolResult, error) {
		argv := FormatArgvSummary(call.Name, call.Input)
		origin, _ := ctx.Value(originatorContextKey{}).(ids.ClientID)

		// Look up the tool's is_mutating declaration via the
		// registry-shared map; we cache it on context. If unset,
		// assume mutating (fail-safe).
		mutating, _ := ctx.Value(mutatingContextKey{}).(bool)

		switch eng.Evaluate(session, call.Name, argv, mutating) {
		case permission.DecisionAllow:
			return next(ctx, call, sink)
		case permission.DecisionDeny:
			return denied(call, "denied by permission policy")
		}

		// DecisionAsk: publish PermissionRequested and wait.
		_, _ = bus.Publish(control.PermissionRequested{
			CallID:     call.ID,
			Tool:       call.Name,
			Args:       call.Input,
			Originator: origin,
			Reason:     "ask-mode tool requires user approval",
		}, origin)

		var answerCh <-chan PermissionAnswer
		if router != nil {
			answerCh = router.Subscribe(session, call.ID)
			defer router.Unsubscribe(session, call.ID)
		}

		timer := time.NewTimer(timeout)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			return denied(call, "turn cancelled")
		case <-timer.C:
			_, _ = bus.Publish(control.Error{
				Reason:  control.ReasonPermissionTimeout,
				Message: "permission for " + call.Name + " not answered within " + timeout.String(),
			}, origin)
			return denied(call, "permission timeout")
		case ans, ok := <-answerCh:
			if !ok {
				return denied(call, "permission channel closed")
			}
			if !ans.Allow {
				return denied(call, "denied by user")
			}
			// Apply scope: session-rule for one-session scopes,
			// persistent-rule for "Allow always".
			rule, persist, ok := permission.AnswerToRuleWithPersist(call.Name, argv, control.AnswerPermission{
				Scope:    ans.Scope,
				Decision: control.DecisionAllow,
			})
			if ok {
				if persist {
					// Failure to persist is logged (Error event) but
					// doesn't block the call — user already approved.
					if err := eng.AddPersistentRule(rule); err != nil {
						_, _ = bus.Publish(control.Error{
							Reason:  "PermissionPersistFailed",
							Message: err.Error(),
						}, origin)
					}
				} else {
					eng.AddSessionRule(session, rule)
				}
			}
			return next(ctx, call, sink)
		}
	}
}

// context-key types — unexported so middleware contracts are explicit.
type originatorContextKey struct{}
type mutatingContextKey struct{}

// WithOriginator stamps a context with the originator ClientID. The
// engine loop applies this when invoking the dispatcher for a turn.
func WithOriginator(ctx context.Context, id ids.ClientID) context.Context {
	return context.WithValue(ctx, originatorContextKey{}, id)
}

// WithMutatingFlag stamps a context with the tool's is_mutating declaration.
// Used by the dispatcher before calling the middleware chain.
func WithMutatingFlag(ctx context.Context, mutating bool) context.Context {
	return context.WithValue(ctx, mutatingContextKey{}, mutating)
}

func denied(call tool.ToolCall, msg string) (*tool.ToolResult, error) {
	return &tool.ToolResult{
		IsError: true,
		Content: []control.ContentBlock{{Type: "text", Text: msg}},
	}, nil
}

// ErrAnswerChannelClosed is returned when the multiplexer's answer
// channel closes without delivering an answer.
var ErrAnswerChannelClosed = errors.New("engine: permission answer channel closed")
