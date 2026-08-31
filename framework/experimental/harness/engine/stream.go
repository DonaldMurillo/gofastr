package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/provider"
)

// StreamCollector consumes a Provider's StreamEvent channel, publishes
// canonical control.Events to the per-session bus, and returns a
// summary describing what the model emitted in the turn.
//
// The summary feeds the loop's "decide loop-or-yield" step:
//
//   - If ToolUses is non-empty, the loop dispatches them and feeds
//     ToolResults back as input.
//   - Otherwise the loop yields (text-only response) unless
//     FinishReason is "length" / "error", in which case the loop
//     surfaces an Error event and yields.
type StreamSummary struct {
	Text         string            // concatenated TextDelta payloads
	Thinking     []json.RawMessage // provider-stamped thinking blocks
	ToolUses     []control.ToolUse // any tool_use blocks the model emitted
	Usage        provider.Usage    // final accounting at message_stop
	FinishReason string            // raw provider finish_reason
}

// CollectStream pumps events from a Provider stream channel into the
// per-session Bus and returns the summary when the channel closes (or
// ctx is done).
func CollectStream(
	ctx context.Context,
	bus *Bus,
	originator ids.ClientID,
	stream <-chan provider.StreamEvent,
) (StreamSummary, error) {
	var (
		summary     StreamSummary
		textBuf     strings.Builder
		curTool     *control.ToolUse
		curToolJSON strings.Builder
		// sawTerminal records that the provider actually ended the turn
		// (a KindStop; KindError returns from inside the loop). Without
		// it, a channel that simply closed -- a dropped connection, a
		// killed subprocess, a truncated SSE body -- returned the partial
		// text with FinishReason "" and a nil error, which is exactly
		// what a complete turn with no finish reason looks like. The
		// caller then recorded a truncated assistant turn as finished.
		sawTerminal bool
	)
	flushTool := func() {
		if curTool != nil {
			// Validate the accumulated JSON, and when it is not valid
			// keep the raw bytes somewhere they can still be marshalled.
			//
			// The raw fragment cannot be stored as the Input directly.
			// A truncated `{"path":"/et` is not JSON, so the moment it
			// lands in History or a ToolCallStarted event, every later
			// marshal of that value fails: the next provider request
			// errors out and the session is stuck until history is
			// trimmed by hand, and the persistence layer drops the
			// tool-intent row instead of writing it. Wrapping the
			// fragment in a valid object keeps the whole pipeline
			// working AND keeps the bytes, which is what lets the model
			// be told its call was cut off.
			s := strings.TrimSpace(curToolJSON.String())
			if s == "" {
				s = "{}"
			}
			if !json.Valid([]byte(s)) {
				wrapped, err := json.Marshal(map[string]string{truncatedInputKey: s})
				if err != nil {
					// A string always marshals; this cannot fail. Fall
					// back to an empty object rather than storing bytes
					// that break every downstream marshal.
					wrapped = []byte("{}")
				}
				s = string(wrapped)
			}
			curTool.Input = json.RawMessage(s)
			summary.ToolUses = append(summary.ToolUses, *curTool)
			curTool = nil
			curToolJSON.Reset()
		}
	}
	for {
		select {
		case <-ctx.Done():
			flushTool()
			return summary, ctx.Err()
		case ev, ok := <-stream:
			if !ok {
				flushTool()
				summary.Text = textBuf.String()
				if !sawTerminal {
					return summary, ErrStreamClosed
				}
				return summary, nil
			}
			switch ev.Kind {
			case provider.KindTextDelta:
				if ev.Text == "" {
					continue
				}
				textBuf.WriteString(ev.Text)
				_, _ = bus.Publish(control.TextDelta{Text: ev.Text}, originator)
			case provider.KindThinkingDelta:
				if len(ev.Thinking) == 0 {
					continue
				}
				summary.Thinking = append(summary.Thinking, json.RawMessage(ev.Thinking))
				_, _ = bus.Publish(control.ThinkingDelta{Block: json.RawMessage(ev.Thinking)}, originator)
			case provider.KindToolUseStart:
				// Adapter has given us name + id; arguments arrive in deltas.
				flushTool() // unusual but safe
				if ev.ToolUse != nil {
					tu := *ev.ToolUse
					curTool = &tu
				}
			case provider.KindToolUseDelta:
				if curTool == nil {
					continue
				}
				curToolJSON.WriteString(ev.InputDelta)
			case provider.KindToolUseStop:
				flushTool()
			case provider.KindUsage:
				if ev.Usage != nil {
					summary.Usage = *ev.Usage
				}
			case provider.KindStop:
				summary.FinishReason = ev.FinishReason
				sawTerminal = true
			case provider.KindError:
				_, _ = bus.Publish(control.Error{
					Reason:  control.ReasonInvalidCommand,
					Message: errOrEmpty(ev.Err),
				}, originator)
				flushTool()
				summary.Text = textBuf.String()
				return summary, ev.Err
			}
		}
	}
}

func errOrEmpty(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// truncatedInputKey is the field a cut-off tool-call argument fragment is
// preserved under. It is deliberately conspicuous: a tool receiving it
// should refuse the call, and a model shown it can see its own arguments
// were truncated rather than guessing from a silent empty object.
const truncatedInputKey = "__gofastr_truncated_input"

// ErrStreamClosed is returned by CollectStream when the provider
// stream closed unexpectedly.
var ErrStreamClosed = errors.New("engine: provider stream closed unexpectedly")
