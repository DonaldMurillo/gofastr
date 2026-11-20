package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool/permission"
)

// PermissionAnswer is a client's answer to one PermissionRequested.
type PermissionAnswer struct {
	CallID ids.CallID
	Allow  bool
	Scope  control.PermitScope
	Source ids.ClientID
}

// AnswerRouter delivers the answers for one pending permission call.
type AnswerRouter interface {
	Subscribe(session ids.SessionID, callID ids.CallID) <-chan PermissionAnswer
	Unsubscribe(session ids.SessionID, callID ids.CallID)
}

// PermissionMiddleware gates every tool call on eng, asking the session's clients when it says ask; timeout 0 means 60s and a negative timeout panics.
func PermissionMiddleware(
	bus *Bus,
	eng *permission.Engine,
	router AnswerRouter,
	session ids.SessionID,
	timeout time.Duration,
) tool.Middleware {
	if timeout < 0 {
		panic(fmt.Sprintf("engine: PermissionMiddleware: timeout must be >= 0 (got %v)", timeout))
	}
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return func(ctx context.Context, call tool.ToolCall, sink tool.EventSink, next tool.Handler) (*tool.ToolResult, error) {
		argv := FormatArgvSummary(call.Name, call.Input)
		origin, _ := ctx.Value(originatorContextKey{}).(ids.ClientID)

		mutating, _ := ctx.Value(mutatingContextKey{}).(bool)

		switch eng.Evaluate(session, call.Name, argv, mutating) {
		case permission.DecisionAllow:
			return next(ctx, call, sink)
		case permission.DecisionDeny:
			return denied(call, "denied by permission policy")
		}

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
			rule, persist, ok := permission.AnswerToRuleWithPersist(call.Name, argv, control.AnswerPermission{
				Scope:    ans.Scope,
				Decision: control.DecisionAllow,
			})
			if ok {
				if persist {
					if err := eng.AddPersistentRule(rule); err != nil {
						log.Printf("engine: persist permission rule: %v", err)
						_, _ = bus.Publish(control.Error{
							Reason:  "PermissionPersistFailed",
							Message: "persisting permission rule failed",
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

type originatorContextKey struct{}
type mutatingContextKey struct{}

// WithOriginator stamps ctx with the client that started the turn.
func WithOriginator(ctx context.Context, id ids.ClientID) context.Context {
	return context.WithValue(ctx, originatorContextKey{}, id)
}

// WithMutatingFlag stamps ctx with whether the tool mutates.
func WithMutatingFlag(ctx context.Context, mutating bool) context.Context {
	return context.WithValue(ctx, mutatingContextKey{}, mutating)
}

func denied(call tool.ToolCall, msg string) (*tool.ToolResult, error) {
	return &tool.ToolResult{
		IsError: true,
		Content: []control.ContentBlock{{Type: "text", Text: msg}},
	}, nil
}

// ErrAnswerChannelClosed names an answer channel that closed without an answer.
var ErrAnswerChannelClosed = errors.New("engine: permission answer channel closed")
